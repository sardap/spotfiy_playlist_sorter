package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	b64 "encoding/base64"

	"github.com/gin-gonic/gin"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
)

var (
	auth     spotify.Authenticator
	state    string
	notFound error
	domain   string
)

func init() {
	auth = spotify.NewAuthenticator(
		os.Getenv("SPOTIFY_REDIRECT_URL"),
		spotify.ScopeUserReadPrivate,
		spotify.ScopeUserLibraryModify,
		spotify.ScopePlaylistReadPrivate,
		spotify.ScopePlaylistReadCollaborative,
		spotify.ScopePlaylistModifyPrivate,
		spotify.ScopePlaylistModifyPublic,
	)

	auth.SetAuthInfo(
		os.Getenv("SPOTIFY_CLIENT_ID"),
		os.Getenv("SPOTIFY_CLIENT_SECRET"),
	)

	notFound = fmt.Errorf("Not found")

	domain = os.Getenv("DOMAIN")
}

type apiRequest struct {
	AccessToken string `json:"access_token"`
}

type playlistRequest struct {
	PlaylistID   string `json:"playlist_id"`
	PlaylistName string `json:"playlist_name"`
}

func removeTracks(
	client spotify.Client,
	pid spotify.ID,
	tracks []spotify.ID,
) error {
	var max int
	for i := 0; i < len(tracks); i += max {
		if i+100 < len(tracks) {
			max = i + 100
		} else {
			max = len(tracks)
		}

		_, err := client.RemoveTracksFromPlaylist(pid, tracks[i:max]...)
		if err != nil {
			return err
		}
		tracks = tracks[max:]
	}

	return nil
}

func purge(
	client spotify.Client,
	start time.Time,
	end time.Time,
	pid spotify.ID,
) error {
	var toRemove []spotify.ID

	tracks, err := client.GetPlaylistTracks(pid)
	if err != nil {
		return err
	}

	looped := false
	for tracks.Next != "" || !looped {
		looped = true
		for _, track := range tracks.Tracks {
			t := track.Track.Album.ReleaseDateTime()
			if t.Before(start) || t.After(end) {
				fmt.Printf("%s\n", track.Track.Album.ReleaseDate)
				toRemove = append(toRemove, track.Track.ID)
			}
		}
		client.NextPage(tracks)
	}

	return removeTracks(client, pid, toRemove)
}

func getPlaylistByName(
	client spotify.Client,
	name string,
) (spotify.SimplePlaylist, error) {
	// the client can now be used to make authenticated requests
	user, _ := client.CurrentUser()

	psRes, err := client.GetPlaylistsForUser(user.ID)
	if err != nil {
		return spotify.SimplePlaylist{}, err
	}

	var result *spotify.SimplePlaylist
	for psRes.Next != "" {
		for _, val := range psRes.Playlists {
			if val.Name == name {
				result = &val
				break
			}
		}

		client.NextPage(psRes)
	}
	if result == nil {
		return spotify.SimplePlaylist{}, notFound
	}

	return *result, nil
}

func clonePlaylist(
	client spotify.Client,
	sourcePlaylist spotify.ID,
	newName string,
) error {

	user, _ := client.CurrentUser()

	playlist, err := client.GetPlaylist(sourcePlaylist)
	if err != nil {
		return err
	}

	newPid, err := client.CreatePlaylistForUser(
		user.ID,
		newName,
		"",
		playlist.IsPublic,
	)
	if err != nil {
		return err
	}

	tracks, err := client.GetPlaylistTracks(sourcePlaylist)
	if err != nil {
		return err
	}

	var newTracks []spotify.ID
	looped := false
	for tracks.Next != "" || !looped {
		looped = true
		for _, track := range tracks.Tracks {
			newTracks = append(newTracks, track.Track.ID)
		}

		client.AddTracksToPlaylist(newPid.ID, newTracks...)
		client.NextPage(tracks)
		newTracks = make([]spotify.ID, 0)
	}

	return nil
}

func getClientFromContex(c *gin.Context) (spotify.Client, error) {
	tkn, err := c.Cookie("token")
	if err != nil {
		return spotify.Client{}, err
	}
	fmt.Printf("token:%s\n", tkn)

	var authToken oauth2.Token
	err = json.Unmarshal([]byte(tkn), &authToken)
	if err != nil {
		return spotify.Client{}, err
	}
	return auth.NewClient(&authToken), nil
}

func getClientFromAccessToken(code string) (spotify.Client, error) {
	str, _ := b64.StdEncoding.DecodeString(code)

	var authToken oauth2.Token
	err := json.Unmarshal([]byte(str), &authToken)
	if err != nil {
		return spotify.Client{}, err
	}

	return auth.NewClient(&authToken), nil
}

// the user will eventually be redirected back to your redirect URL
// typically you'll have a handler set up like the following:
func redirectHandler(c *gin.Context) {
	// use the same state string here that you used to generate the URL
	token, err := auth.Token(state, c.Request)
	if err != nil {
		http.Error(c.Writer, "Couldn't get token", http.StatusNotFound)
		return
	}

	blob, _ := json.Marshal(token)
	c.SetCookie(
		"token", string(blob), 60*60, "/", domain, false, true,
	)

	c.JSON(200, gin.H{
		"access_token": b64.StdEncoding.EncodeToString(blob),
	})
}

func loginEndpoint(c *gin.Context) {
	url := auth.AuthURL(state)
	c.Redirect(http.StatusPermanentRedirect, url)
}

type cloneRequest struct {
	apiRequest
	playlistRequest
	NewName string `json:"new_name"`
}

func cloneEndpoint(c *gin.Context) {
	var request cloneRequest
	body, _ := ioutil.ReadAll(c.Request.Body)
	json.Unmarshal(body, &request)

	var err error
	var client spotify.Client
	if request.AccessToken == "" {
		client, err = getClientFromContex(c)
	} else {
		client, err = getClientFromAccessToken(request.AccessToken)
	}
	if err != nil {
		c.JSON(401, gin.H{
			"message": "re auth",
		})
		return
	}

	var playlist spotify.SimplePlaylist
	if request.PlaylistID != "" {
		pres, err := client.GetPlaylist(spotify.ID(request.PlaylistID))
		if err != nil {
			c.JSON(404, gin.H{
				"message": "playlist with name not found",
			})
			return
		}
		playlist = pres.SimplePlaylist
	} else {
		playlist, err = getPlaylistByName(client, request.PlaylistName)
		if err != nil {
			if err == notFound {
				c.JSON(404, gin.H{
					"message": "playlist with name not found",
				})
				return
			}
			c.JSON(500, gin.H{})
			return
		}
	}

	var newName string
	if request.NewName != "" {
		newName = request.NewName
	} else {
		newName = fmt.Sprintf("Clone of %s", playlist.Name)
	}

	clonePlaylist(client, playlist.ID, newName)
}

type purgeRequest struct {
	apiRequest
	playlistRequest
	//format 2000-11-21
	Start string `json:"start_time"`
	//format 2004-10-13
	End string `json:"end_time"`
	//One or the other
}

func purgeEndpoint(c *gin.Context) {
	var request purgeRequest
	body, _ := ioutil.ReadAll(c.Request.Body)
	json.Unmarshal(body, &request)

	var err error
	var client spotify.Client
	if request.AccessToken == "" {
		client, err = getClientFromContex(c)
	} else {
		client, err = getClientFromAccessToken(request.AccessToken)
	}
	if err != nil {
		c.JSON(401, gin.H{
			"message": "re auth",
		})
		return
	}

	var playlist spotify.SimplePlaylist
	if request.PlaylistID != "" {
		pres, err := client.GetPlaylist(spotify.ID(request.PlaylistID))
		if err != nil {
			c.JSON(404, gin.H{
				"message": "playlist with name not found",
			})
			return
		}
		playlist = pres.SimplePlaylist
	} else {
		playlist, err = getPlaylistByName(client, request.PlaylistName)
		if err != nil {
			if err == notFound {
				c.JSON(404, gin.H{
					"message": "playlist with name not found",
				})
				return
			}
			c.JSON(500, gin.H{})
			return
		}
	}

	start, err := time.Parse("2006-01-02", request.Start)
	if err != nil {
		c.JSON(400, gin.H{
			"message": "invalid start_time format",
		})
		return
	}
	cutoff, err := time.Parse("2006-01-02", request.End)
	if err != nil {
		c.JSON(400, gin.H{
			"message": "invalid end_time format",
		})
		return
	}
	err = purge(client, start, cutoff, playlist.ID)
	if err != nil {
		if err == notFound {
			c.JSON(404, gin.H{})
		} else {
			c.JSON(500, gin.H{})
		}
		return
	}

	c.JSON(200, gin.H{})
}

func main() {
	// get the user to this URL - how you do that is up to you
	// you should specify a unique state string to identify the session

	r := gin.Default()
	r.GET("/login", loginEndpoint)
	r.GET("/callback", redirectHandler)
	r.GET("/api/v1/accessToken", loginEndpoint)
	r.PATCH("/api/v1/purge", purgeEndpoint)
	r.POST("/api/v1/clone", cloneEndpoint)
	r.Run(":8888") // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}
