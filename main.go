package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
)

const (
	totoID = "2374M0fQpWi3dLnB54qaLX"
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

type sortRule struct {
	FeatureName string `json:"feature_name"`
	Descending  bool   `json:"descending"`
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

	for {
		var newTracks []spotify.ID
		for _, track := range tracks.Tracks {
			newTracks = append(newTracks, track.Track.ID)
		}

		client.AddTracksToPlaylist(newPid.ID, newTracks...)

		if tracks.Next == "" {
			break
		}
		client.NextPage(tracks)
	}

	return nil
}

type trackFun func(tracksSubset []spotify.ID)

//Todo better name please
func doForAll(ids []spotify.ID, fun trackFun) {
	for i := 0; i < len(ids); i += 100 {
		end := i + 100

		if end > len(ids) {
			end = len(ids)
		}

		fun(ids[i:end])
	}
}

type trackComplete struct {
	spotify.AudioFeatures
	Popularity float64
}

func sortVal(rule sortRule, features *trackComplete) float64 {
	result := float64(0)
	var val float64
	switch rule.FeatureName {
	case "popularity":
		val = float64(features.Popularity)
	case "danceability":
		val = float64(features.Danceability)
		break
	case "acousticness":
		val = float64(features.Acousticness)
		break
	case "energy":
		val = float64(features.Energy)
		break
	case "key":
		val = float64(features.Key)
		break
	case "loudness":
		val = float64(features.Loudness)
		break
	case "mode":
		val = float64(features.Mode)
		break
	case "instrumentalness":
		val = float64(features.Instrumentalness)
		break
	case "liveness":
		val = float64(features.Liveness)
		break
	case "valence":
		val = float64(features.Valence)
		break
	case "tempo":
		val = float64(features.Tempo)
		break
	case "duration_ms":
		val = float64(features.Duration)
		break
	}

	if rule.Descending {
		val = -val
	}

	result += val

	return result
}

func sortBy(
	client spotify.Client,
	sourcePlaylist spotify.ID,
	sortBy []sortRule,
) error {
	tracks, err := client.GetPlaylistTracks(sourcePlaylist)
	if err != nil {
		return err
	}

	var features []*trackComplete
	var allTrackIds []spotify.ID
	for {
		var trackIds []spotify.ID
		for _, track := range tracks.Tracks {
			trackIds = append(trackIds, track.Track.ID)
		}
		allTrackIds = append(allTrackIds, trackIds...)

		tmp, _ := client.GetAudioFeatures(trackIds...)
		for i, feature := range tmp {
			if feature == nil {
				continue
			}
			//Gross and dumb
			features = append(features, &trackComplete{
				AudioFeatures: spotify.AudioFeatures{
					Acousticness:     feature.Acousticness,
					AnalysisURL:      feature.AnalysisURL,
					Danceability:     feature.Danceability,
					Duration:         feature.Duration,
					Energy:           feature.Energy,
					ID:               feature.ID,
					Instrumentalness: feature.Instrumentalness,
					Key:              feature.Key,
					Liveness:         feature.Liveness,
					Loudness:         feature.Loudness,
					Mode:             feature.Mode,
					Speechiness:      feature.Speechiness,
					Tempo:            feature.Tempo,
					TimeSignature:    feature.TimeSignature,
					TrackURL:         feature.TrackURL,
					URI:              feature.URI,
					Valence:          feature.Valence,
				},
				Popularity: float64(tracks.Tracks[i].Track.Popularity),
			})
		}

		if tracks.Next == "" {
			break
		} else {
			client.NextPage(tracks)
		}
	}

	sort.SliceStable(features, func(i, j int) bool {
		var leftVal, rightVal float64
		for _, rule := range sortBy {
			left := sortVal(rule, features[i])
			right := sortVal(rule, features[j])

			leftVal += left
			rightVal += right

			if left != right {
				break
			}
		}

		return leftVal < rightVal
	})

	//Sorted order
	var sortedTrackIds []spotify.ID
	for _, f := range features {
		sortedTrackIds = append(sortedTrackIds, f.ID)
	}

	doForAll(allTrackIds, func(ids []spotify.ID) {
		client.RemoveTracksFromPlaylist(sourcePlaylist, ids...)
	})

	doForAll(sortedTrackIds, func(ids []spotify.ID) {
		client.AddTracksToPlaylist(sourcePlaylist, ids...)
	})

	return nil
}

func noTotoAfrica(
	client spotify.Client,
) error {
	usr, _ := client.CurrentUser()

	pRes, err := client.GetPlaylistsForUser(usr.ID)
	if err != nil {
		return err
	}

	for {
		for _, playlist := range pRes.Playlists {
			client.RemoveTracksFromPlaylist(playlist.ID, totoID)
		}

		if err := client.NextPage(pRes); err != nil {
			break
		}
	}

	return client.RemoveTracksFromLibrary(totoID)
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

	if err := noTotoAfrica(client); err != nil {
		log.Printf("NoToto, requestBody:%s", body)
		return
	}

	c.JSON(204, gin.H{})
}

func validRule(rule sortRule) error {
	str := rule.FeatureName
	if str != "danceability" &&
		str != "energy" &&
		str != "key" &&
		str != "loudness" &&
		str != "mode" &&
		str != "speechiness" &&
		str != "acousticness" &&
		str != "instrumentalness" &&
		str != "liveness" &&
		str != "valence" &&
		str != "tempo" &&
		str != "popularity" &&
		str != "duration_ms" {
		return fmt.Errorf("invalid sorting feature")
	}

	return nil
}

type sortRequest struct {
	apiRequest
	playlistRequest
	SortBy []sortRule `json:"sort_rules"`
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

	for _, rule := range request.SortBy {
		if err := validRule(rule); err != nil {
			c.JSON(400, gin.H{
				"message": err.Error(),
			})
		}
	}

	sortBy(client, playlist.ID, request.SortBy)

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

	clonePlaylist(client, playlist.ID, newName)

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
	err = purge(client, start, cutoff, playlist.ID)
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
	r.Run(":8888") // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}
