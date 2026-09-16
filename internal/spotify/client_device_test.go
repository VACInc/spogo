package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlaybackResolvesConfiguredDeviceName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/me/player/devices", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(deviceResponse{Devices: []deviceItem{{ID: "device-id", Name: "Desk Speaker"}}})
	})
	mux.HandleFunc("/me/player/play", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("device_id"); got != "device-id" {
			t.Fatalf("device_id = %q, want device-id", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewClient(Options{TokenProvider: staticTokenProvider{}, BaseURL: srv.URL, Device: "Desk Speaker"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := client.Play(context.Background(), "spotify:track:t1"); err != nil {
		t.Fatalf("play: %v", err)
	}
}

func TestConfiguredDeviceDoesNotLeakToNonPlaybackMutation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/me/tracks", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("device_id"); got != "" {
			t.Fatalf("unexpected device_id %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewClient(Options{TokenProvider: staticTokenProvider{}, BaseURL: srv.URL, Device: "Desk Speaker"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := client.LibraryModify(context.Background(), "/me/tracks", []string{"t1"}, http.MethodPut); err != nil {
		t.Fatalf("library modify: %v", err)
	}
}

func TestPlaybackPassesThroughDeviceIDWithoutLookup(t *testing.T) {
	const deviceID = "fc54640849c38ae79f37b6b7f13185bb04a1989d"
	deviceCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/me/player/devices", func(http.ResponseWriter, *http.Request) {
		deviceCalls++
	})
	mux.HandleFunc("/me/player/play", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("device_id"); got != deviceID {
			t.Errorf("device_id = %q, want %q", got, deviceID)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewClient(Options{TokenProvider: staticTokenProvider{}, BaseURL: srv.URL, Device: deviceID})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := client.Play(context.Background(), "spotify:track:t1"); err != nil {
		t.Fatalf("play: %v", err)
	}
	if deviceCalls != 0 {
		t.Fatalf("device lookup calls = %d, want 0", deviceCalls)
	}
}

func TestPlaybackPreservesDeviceLookupAPIErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/me/player/devices", func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
			})
			mux.HandleFunc("/me/player/play", func(http.ResponseWriter, *http.Request) {
				t.Fatal("play request should not run after device lookup failure")
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			client, err := NewClient(Options{TokenProvider: staticTokenProvider{}, BaseURL: srv.URL, Device: "Desk Speaker"})
			if err != nil {
				t.Fatalf("client: %v", err)
			}
			err = client.Play(context.Background(), "spotify:track:t1")
			var apiErr APIError
			if !errors.As(err, &apiErr) || apiErr.Status != test.status {
				t.Fatalf("error = %v, want API status %d", err, test.status)
			}
			if test.retryAfter != "" && apiErr.RetryAfter != 42*time.Second {
				t.Fatalf("retry after = %s, want 42s", apiErr.RetryAfter)
			}
		})
	}
}

func TestPlaybackPreservesCanceledDeviceLookup(t *testing.T) {
	client, err := NewClient(Options{TokenProvider: staticTokenProvider{}, BaseURL: "http://127.0.0.1:1", Device: "Desk Speaker"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Play(ctx, "spotify:track:t1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestPlaybackRejectsNamedDeviceWithoutID(t *testing.T) {
	playCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/me/player/devices", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":null,"name":"Desk Speaker"}]}`))
	})
	mux.HandleFunc("/me/player/play", func(http.ResponseWriter, *http.Request) {
		playCalled = true
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewClient(Options{TokenProvider: staticTokenProvider{}, BaseURL: srv.URL, Device: "Desk Speaker"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	err = client.Play(context.Background(), "spotify:track:t1")
	if err == nil || err.Error() != `device "Desk Speaker" has no usable ID` {
		t.Fatalf("error = %v, want unusable device ID error", err)
	}
	if playCalled {
		t.Fatal("play request should not run for a device without an ID")
	}
}
