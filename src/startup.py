import argparse
import requests
import json
import editdistance
import random

def get_header(access_token):
	return {
		'Content-Type': "application/json",
		'Authorization': "Bearer {}".format(access_token),
		'Connection': "keep-alive",
		'cache-control': "no-cache"
	}

def reverse_tracks(tracks):
	return tracks[::-1]

def remove_tracks_from_playlist(access_token, play_id, tracks):
	print("clearing tracks from {}".format(play_id))
	url = "https://api.spotify.com/v1/playlists/{}/tracks".format(play_id)

	request_ary = []
	for i in tracks:
		request_ary.append({ "uri" : "spotify:track:{}".format(i["track"]["id"]) })

	request_playload_json = { "tracks" : request_ary }
	
	payload = json.dumps(request_playload_json)
	headers = get_header(access_token)

	response = requests.request("DELETE", url, data=payload, headers=headers)

	return response.status_code == 200

def add_track_to_playlist(access_token, play_id, track_id):
	url = "https://api.spotify.com/v1/playlists/{}/tracks?uris=spotify:track:{}".format(play_id, track_id)

	headers = get_header(access_token)

	response = requests.request("POST", url, headers=headers)

	if response.status_code != 200:
		return add_track_to_playlist(access_token, play_id, track_id)
	else:
		return True

def add_tracks_to_playlist(access_token, tracks_ids, play_id):

	url = "https://api.spotify.com/v1/playlists/" + play_id + "/tracks"

	payload = ""
	headers = get_header(access_token)
	
	while len(tracks_ids) > 0:
		request_songs = ""
		for i in range(0, min(90, len(tracks_ids))):
			request_songs += "spotify:track:" + tracks_ids[0][0] + ","
			tracks_ids.pop(0)

		request_songs = request_songs[:-1]

		querystring = {"uris" : request_songs}

		response = requests.request("POST", url, data=payload, headers=headers, params=querystring)

def get_track_features(access_token, track_id):
	url = "https://api.spotify.com/v1/audio-features/" + track_id

	payload = ""
	headers = get_header(access_token)

	response = requests.request("GET", url, data=payload, headers=headers, timeout=None)
	
	if(response.status_code != 200):
		print("failled to get feature for track: {}".format(track_id))
		return None
	else:
		return json.loads(response.text)

def sort_tracks_by_feature(access_token, tracks, feature, max_length=-1):
	feature_dict = {}
	print("Sorting {} songs".format(len(tracks)))

	random.shuffle(tracks)
	total_length = 0

	while total_length < max_length and len(tracks) > 0:
		track = tracks.pop(0)

		track_id = track["track"]["id"]
		print("Getting {} for: {}".format(feature, track))
		features = get_track_features(access_token, track_id)

		if features == None:
			continue

		value = features[feature]
		print("Value of {} is {}".format(track_id, value))
		feature_dict[track_id] = value
		
		total_length += features["duration_ms"]
		print("Total Track length {}ms".format(total_length))
	

	sorted_tracks = sorted(feature_dict.items(), key=lambda kv: kv[1])

	print("playlist sorted {}".format(sorted_tracks))

	return sorted_tracks

def tracks_in_playlist(access_token, play_id, n):

	payload = ""
	headers = get_header(access_token)
	
	offset = 0

	result = []

	while(offset < n):
		url = "https://api.spotify.com/v1/playlists/{}/tracks?tracks=100&fields=items(track(id))&offset={}".format(play_id, offset)
		response = requests.request("GET", url, data=payload, headers=headers)
		offset += 100

		for i in json.loads(response.text)["items"]:
			result.append(i)

	print("Playlist gotten")
	return result

def create_playlist(access_token, name):
	print("playlist created")
	url = "https://api.spotify.com/v1/me/playlists"

	payload = "{\r\n\t\"name\": \"" + name + "\",\r\n\t\"description\": \"New playlist description\",\r\n\t\"public\": false\r\n}"
	headers = get_header(access_token)

	response = requests.request("POST", url, data=payload, headers=headers)

	return json.loads(response.text)["id"]

def get_playlist_tracks(access_token, play_id, n):

	payload = ""
	headers = get_header(access_token)
	
	offset = 0

	result = []

	while(offset < n):
		url = "https://api.spotify.com/v1/playlists/{}/tracks?tracks=100&fields=items(track(id))&offset={}".format(play_id, offset)
		response = requests.request("GET", url, data=payload, headers=headers)
		offset += 100

		for i in json.loads(response.text)["items"]:
			result.append(i)

	print("Playlist {} gotten".format(play_id))
	return result

def get_playlist_by_name(access_token, name):
	print("Getting playlist named {}".format(name))
	url = "https://api.spotify.com/v1/me/playlists"

	headers = get_header(access_token)

	response = requests.request("GET", url, headers=headers)

	if response.status_code != 200:
		print("failled to get playlist by name retrying")
		return get_playlist_by_name(access_token, name)
	else:
		for i in json.loads(response.text)["items"]:
			x = i["name"].lower()
			if editdistance.distance(name.lower(), x.lower()) < 2:
				return i["id"], i["tracks"]["total"]

	return None, None

def parse_arguments():
	parser = argparse.ArgumentParser(description='Sort a Spotify playlist by a feature')
	parser.add_argument('access_token', type=str, help='access token for Spotify')
	parser.add_argument('playlist_name', type=str, help='playlist name')
	parser.add_argument('feature', type=str, help='feature to sort by https://developer.spotify.com/documentation/web-api/reference/tracks/get-audio-features/')
	parser.add_argument('-r', '--reverse', action='store_true', required=False, help='reverse playlist')
	parser.add_argument('-l', '--length', help='max playlist length', default=-1)
	return parser.parse_args()

def main():
	args = parse_arguments()

	playlist_id, playlist_n = get_playlist_by_name(
		args.access_token, 
		args.playlist_name
	)

	if playlist_id == None:
		print("FATAL ERROR: Cannot find playlist")
		return

	track_ids = get_playlist_tracks(
		args.access_token,
		playlist_id,
		playlist_n
	)

	sorted_track_ids = sort_tracks_by_feature(
		args.access_token, 
		track_ids,
		args.feature,
		max_length=int(args.length)
	)
	
	if args.reverse:
		sorted_track_ids = reverse_tracks(sorted_track_ids)

	new_playlist_name = "{}_sorted_by_{}".format(
		''.join([i if ord(i) < 128 else ' ' for i in args.playlist_name]),
		args.feature
	)

	new_playlist_id, new_playlist_tracks_n = get_playlist_by_name(
		args.access_token, 
		new_playlist_name
	)

	if new_playlist_id == None:
		new_playlist_id = create_playlist(
			args.access_token, 
			new_playlist_name
		)
	else:
		track_ids = tracks_in_playlist(
			args.access_token, 
			new_playlist_id, 
			new_playlist_tracks_n
		)
		remove_tracks_from_playlist(
			args.access_token, 
			new_playlist_id, 
			track_ids
		)
	
	add_tracks_to_playlist(
		args.access_token,
		sorted_track_ids,
		new_playlist_id
	)

main()
