package sorter

import (
	"fmt"
	"sort"
	"time"

	"github.com/zmb3/spotify"
)

const (
	totoID = "2374M0fQpWi3dLnB54qaLX"
)

//FeatureName refer to Val function switch to a complete list
type FeatureName string

//SortRule Used to sort a playlist
type SortRule struct {
	Name       FeatureName `json:"feature_name"`
	Descending bool        `json:"descending"`
}

//TrackComplete combines audioFeatures and full track data
type TrackComplete struct {
	spotify.AudioFeatures
	spotify.FullTrack
}

//Val returns the value for the rule from the given track
func (r *SortRule) Val(track TrackComplete) (float64, error) {
	result := float64(0)
	var val float64
	switch r.Name {
	case "release_date":
		val = float64(track.Album.ReleaseDateTime().Unix())
		break
	case "explicit":
		if track.Explicit {
			val = 1
		} else {
			val = 0
		}
		break
	case "popularity":
		val = float64(track.Popularity)
	case "danceability":
		val = float64(track.Danceability)
		break
	case "acousticness":
		val = float64(track.Acousticness)
		break
	case "energy":
		val = float64(track.Energy)
		break
	case "key":
		val = float64(track.Key)
		break
	case "loudness":
		val = float64(track.Loudness)
		break
	case "mode":
		val = float64(track.Mode)
		break
	case "instrumentalness":
		val = float64(track.Instrumentalness)
		break
	case "liveness":
		val = float64(track.Liveness)
		break
	case "valence":
		val = float64(track.Valence)
		break
	case "tempo":
		val = float64(track.Tempo)
		break
	case "duration_ms":
		val = float64(track.Duration)
		break
	default:
		return -1, fmt.Errorf("invalid feature_name in sort rule")
	}

	if r.Descending {
		val = -val
	}

	result += val

	return result, nil
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

//Purge will remove all tracks in a playlist which released outside of the given
//start and end time
func Purge(
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

//ClonePlaylist will clone the given playlist and name it with the given name
func ClonePlaylist(
	client spotify.Client,
	sourcePlaylist spotify.ID,
	newName string,
) error {

	if newName == "" {
		return fmt.Errorf("newName cannot be empty")
	}

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

//SortBy will sort the target playlist by the list of sort rykes
func SortBy(
	client spotify.Client,
	sourcePlaylist spotify.ID,
	sortBy []SortRule,
) error {
	tracks, err := client.GetPlaylistTracks(sourcePlaylist)
	if err != nil {
		return err
	}

	var features []*TrackComplete
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
			features = append(features, &TrackComplete{
				AudioFeatures: *feature,
				FullTrack:     tracks.Tracks[i].Track,
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
			left, _ := rule.Val(*features[i])
			right, _ := rule.Val(*features[j])

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

//NoTotoAfrica will remove the track toto africa from every single playlist
//and libary for the given client.
func NoTotoAfrica(
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
