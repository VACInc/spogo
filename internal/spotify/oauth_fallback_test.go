package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/steipete/spogo/internal/cookies"
)

func TestOAuthFallbackPreservesCookieFailures(t *testing.T) {
	operations := map[string]func(API) error{
		"search":          func(c API) error { _, err := c.Search(context.Background(), "track", "song", 1, 0); return err },
		"info":            func(c API) error { _, err := c.GetTrack(context.Background(), "t1"); return err },
		"tracks":          func(c API) error { _, _, err := c.LibraryTracks(context.Background(), 1, 0); return err },
		"albums":          func(c API) error { _, _, err := c.LibraryAlbums(context.Background(), 1, 0); return err },
		"playlists":       func(c API) error { _, _, err := c.Playlists(context.Background(), 1, 0); return err },
		"playlist tracks": func(c API) error { _, _, err := c.PlaylistTracks(context.Background(), "p1", 1, 0); return err },
		"artists":         func(c API) error { _, _, _, err := c.FollowedArtists(context.Background(), 1, ""); return err },
		"add":             func(c API) error { return c.AddTracks(context.Background(), "p1", []string{"spotify:track:t1"}) },
		"remove":          func(c API) error { return c.RemoveTracks(context.Background(), "p1", []string{"spotify:track:t1"}) },
	}
	for _, cause := range []error{cookies.ErrNoCookies, os.ErrNotExist, os.ErrPermission, APIError{Status: 401}, APIError{Status: 403}} {
		for name, operation := range operations {
			t.Run(cause.Error()+"/"+name, func(t *testing.T) {
				calls := 0
				web, err := NewClient(Options{TokenProvider: staticTokenProvider{}, HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return nil, errors.New("unexpected OAuth request")
				})}})
				if err != nil {
					t.Fatal(err)
				}
				connect, err := NewConnectClient(ConnectOptions{Source: cookieSourceStub{err: cause}, WebClient: web})
				if err != nil {
					t.Fatal(err)
				}
				for _, client := range []API{connect, NewAutoClient(connect, web)} {
					if got := operation(client); !errors.Is(got, cause) {
						t.Fatalf("lost cookie failure: %v", got)
					}
				}
				if calls != 0 {
					t.Fatalf("OAuth requests after cookie failure: %d", calls)
				}
			})
		}
	}
}

func TestAutoControlSkipsOAuthAfterAuthenticationFailure(t *testing.T) {
	webCalls, localCalls := map[string]int{}, map[string]int{}
	connect := apiStub{pauseFn: func(context.Context) error { return cookieAuthenticationError{os.ErrNotExist} }}
	web := apiStub{calls: webCalls}
	local := apiStub{calls: localCalls}
	if err := NewAutoClient(connect, web, local).Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if webCalls["Pause"] != 0 || localCalls["Pause"] != 1 {
		t.Fatalf("web=%v local=%v", webCalls, localCalls)
	}
}

func TestOAuthFallbackAfterTransientConnectBootstrapFailure(t *testing.T) {
	for _, stage := range []string{"app config", "client token"} {
		for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusOK} {
			t.Run(fmt.Sprintf("%s/%d", stage, code), func(t *testing.T) {
				webCalls := 0
				client := newConnectClientForTests(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host == "api.spotify.com" {
						webCalls++
						response := jsonResponse(http.StatusOK, map[string]any{"tracks": map[string]any{"items": []any{}, "total": 0}})
						response.ContentLength = -1
						return response, nil
					}
					// An empty successful response models web-player markup/protocol drift.
					return jsonResponse(code, map[string]any{}), nil
				}))
				client.session.source = cookieSourceStub{cookies: []*http.Cookie{{Name: "sp_t", Value: "synthetic"}}}
				if stage == "app config" {
					client.session.clientVer = ""
				} else {
					client.session.clientToken = ""
				}
				if _, err := client.Search(context.Background(), "track", "song", 1, 0); err != nil {
					t.Fatal(err)
				}
				if webCalls != 1 {
					t.Fatalf("Web fallback requests: %d", webCalls)
				}
			})
		}
	}
}

func TestConnectMissingDeviceCookieIsAuthenticationFailure(t *testing.T) {
	client := newConnectClientForTests(nil)
	client.session.clientVer = ""
	client.session.source = cookieSourceStub{}
	_, err := client.session.auth(context.Background())
	if !isAuthenticationError(err) {
		t.Fatalf("unclassified missing device cookie: %v", err)
	}
}
