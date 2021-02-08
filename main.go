package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sardap/playlist_sorter/sorter"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
)

var (
	auth     spotify.Authenticator
	state    string
	domain   string
	port     string
	notFound error
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

	domain = os.Getenv("DOMAIN")

	notFound = fmt.Errorf("Not found")

	port = os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
}

type apiRequest struct {
	AccessToken string `json:"access_token"`
}

type playlistRequest struct {
	PlaylistID   string `json:"playlist_id"`
	PlaylistName string `json:"playlist_name"`
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

func getClientFromToken(token []byte) (spotify.Client, error) {
	return auth.NewClient(decodeToken(token)), nil
}

func getClientFromContex(c *gin.Context) (spotify.Client, error) {
	tknStr, err := c.Cookie("token")
	if err != nil {
		return spotify.Client{}, err
	}

	return getClientFromToken([]byte(tknStr))
}

func encodeToken(a *oauth2.Token) []byte {
	jsonfied, _ := json.Marshal(*a)

	var result []byte
	result = []byte(base64.StdEncoding.EncodeToString(jsonfied))
	return result
}

func decodeToken(encodedToken []byte) *oauth2.Token {
	token, _ := base64.StdEncoding.DecodeString(string(encodedToken))

	var result oauth2.Token
	json.Unmarshal(token, &result)

	return &result
}

// the user will eventually be redirected back to your redirect URL
// typically you'll have a handler set up like the following:
func redirectHandler(c *gin.Context) {
	// use the same state string here that you used to generate the URL
	code := c.Request.URL.Query().Get("code")
	token, err := auth.Exchange(code)
	if err != nil {
		http.Error(c.Writer, "Couldn't get token", http.StatusNotFound)
		return
	}

	c.SetCookie(
		"token", string(encodeToken(token)), 60*60, "/", domain, false, true,
	)

	c.JSON(200, gin.H{
		"access_token": string(encodeToken(token)),
	})
}

func loginEndpoint(c *gin.Context) {
	url := auth.AuthURL(state)
	c.Redirect(http.StatusPermanentRedirect, url)
}

type noTotoRequest struct {
	apiRequest
	playlistRequest
}

func noTotoAfricaEndpoint(c *gin.Context) {
	var request noTotoRequest
	body, _ := ioutil.ReadAll(c.Request.Body)
	json.Unmarshal(body, &request)

	var err error
	var client spotify.Client
	if request.AccessToken == "" {
		client, err = getClientFromContex(c)
	} else {
		client, err = getClientFromToken([]byte(request.AccessToken))
	}
	if err != nil {
		c.JSON(401, gin.H{
			"message": "re auth",
		})
		return
	}

	if err := sorter.NoTotoAfrica(client); err != nil {
		log.Printf("NoToto, requestBody:%s", body)
		return
	}

	c.JSON(204, gin.H{})
}

type sortRequest struct {
	apiRequest
	playlistRequest
	SortBy []sorter.SortRule `json:"sort_rules"`
}

func sortEndpoint(c *gin.Context) {
	var request sortRequest
	body, _ := ioutil.ReadAll(c.Request.Body)
	json.Unmarshal(body, &request)

	if request.SortBy == nil {
		c.JSON(401, gin.H{
			"message": "missing sort_rules",
		})
		return
	}

	for _, rule := range request.SortBy {
		if _, err := rule.Val(sorter.TrackComplete{}); err != nil {
			c.JSON(400, gin.H{
				"message": err.Error(),
			})
			return
		}
	}

	var err error
	var client spotify.Client
	if request.AccessToken == "" {
		client, err = getClientFromContex(c)
	} else {
		client, err = getClientFromToken([]byte(request.AccessToken))
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

	sorter.SortBy(client, playlist.ID, request.SortBy)

	c.JSON(204, gin.H{})
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
		client, err = getClientFromToken([]byte(request.AccessToken))
	}
	if err != nil {
		log.Printf("Error Auth: %s", err)
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

	sorter.ClonePlaylist(client, playlist.ID, newName)

	c.JSON(204, gin.H{})
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
		client, err = getClientFromToken([]byte(request.AccessToken))
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
	err = sorter.Purge(client, start, cutoff, playlist.ID)
	if err != nil {
		if err == notFound {
			c.JSON(404, gin.H{})
		} else {
			c.JSON(500, gin.H{})
		}
		return
	}

	c.JSON(204, gin.H{})
}

func main() {
	// get the user to this URL - how you do that is up to you
	// you should specify a unique state string to identify the session

	r := gin.Default()
	r.GET("/login", loginEndpoint)
	r.GET("/callback", redirectHandler)
	r.GET("/api/v1/accessToken", loginEndpoint)
	r.PATCH("/api/v1/purge", purgeEndpoint)
	r.PATCH("/api/v1/sort", sortEndpoint)
	r.POST("/api/v1/clone", cloneEndpoint)
	r.PATCH("/api/v1/noTotoAfrica", noTotoAfricaEndpoint)
	r.Run(fmt.Sprintf(":%s", port)) // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}
