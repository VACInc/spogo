package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) Playback(ctx context.Context) (PlaybackStatus, error) {
	var raw playbackResponse
	if err := c.get(ctx, "/me/player", nil, &raw); err != nil {
		if errors.Is(err, ErrNoContent) {
			return PlaybackStatus{IsPlaying: false}, nil
		}
		return PlaybackStatus{}, err
	}
	status := PlaybackStatus{
		IsPlaying:  raw.IsPlaying,
		ProgressMS: raw.ProgressMS,
		Shuffle:    raw.ShuffleState,
		Repeat:     raw.RepeatState,
		Device:     mapDevice(raw.Device),
	}
	if raw.Item.ID != "" {
		item := mapTrack(raw.Item)
		status.Item = &item
		if itemNeedsTrackMetadata(status.Item) {
			if full, err := c.GetTrack(ctx, status.Item.ID); err == nil {
				mergeItemMetadata(status.Item, full)
			}
		}
	}
	return status, nil
}

func (c *Client) Play(ctx context.Context, uri string) error {
	payload := map[string]any{}
	if uri != "" {
		if isContextURI(uri) {
			payload["context_uri"] = uri
		} else {
			payload["uris"] = []string{uri}
		}
	}
	params, err := c.playbackParams(ctx, nil)
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPut, "/me/player/play", params, payload, nil)
}

func (c *Client) Pause(ctx context.Context) error {
	params, err := c.playbackParams(ctx, nil)
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPut, "/me/player/pause", params, nil, nil)
}

func (c *Client) Next(ctx context.Context) error {
	params, err := c.playbackParams(ctx, nil)
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPost, "/me/player/next", params, nil, nil)
}

func (c *Client) Previous(ctx context.Context) error {
	params, err := c.playbackParams(ctx, nil)
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPost, "/me/player/previous", params, nil, nil)
}

func (c *Client) Seek(ctx context.Context, positionMS int) error {
	params := url.Values{}
	params.Set("position_ms", fmt.Sprint(positionMS))
	params, err := c.playbackParams(ctx, params)
	if err != nil {
		return err
	}
	return c.putParams(ctx, "/me/player/seek", params)
}

func (c *Client) Volume(ctx context.Context, volume int) error {
	params := url.Values{}
	params.Set("volume_percent", fmt.Sprint(volume))
	params, err := c.playbackParams(ctx, params)
	if err != nil {
		return err
	}
	return c.putParams(ctx, "/me/player/volume", params)
}

func (c *Client) Shuffle(ctx context.Context, enabled bool) error {
	params := url.Values{}
	params.Set("state", fmt.Sprint(enabled))
	params, err := c.playbackParams(ctx, params)
	if err != nil {
		return err
	}
	return c.putParams(ctx, "/me/player/shuffle", params)
}

func (c *Client) Repeat(ctx context.Context, mode string) error {
	params := url.Values{}
	params.Set("state", mode)
	params, err := c.playbackParams(ctx, params)
	if err != nil {
		return err
	}
	return c.putParams(ctx, "/me/player/repeat", params)
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var raw deviceResponse
	if err := c.get(ctx, "/me/player/devices", nil, &raw); err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(raw.Devices))
	for _, d := range raw.Devices {
		devices = append(devices, mapDevice(d))
	}
	return devices, nil
}

func (c *Client) Transfer(ctx context.Context, deviceID string) error {
	payload := map[string]any{"device_ids": []string{deviceID}}
	return c.put(ctx, "/me/player", payload)
}

func (c *Client) QueueAdd(ctx context.Context, uri string) error {
	params := url.Values{}
	params.Set("uri", uri)
	params, err := c.playbackParams(ctx, params)
	if err != nil {
		return err
	}
	return c.postParams(ctx, "/me/player/queue", params)
}

func (c *Client) playbackParams(ctx context.Context, params url.Values) (url.Values, error) {
	if c.device == "" {
		return params, nil
	}
	if params == nil {
		params = url.Values{}
	}
	if params.Get("device_id") != "" {
		return params, nil
	}
	if looksLikeDeviceID(c.device) {
		params.Set("device_id", c.device)
		return params, nil
	}
	devices, err := c.Devices(ctx)
	if err != nil {
		return nil, err
	}
	for _, device := range devices {
		if strings.EqualFold(device.ID, c.device) || strings.EqualFold(device.Name, c.device) {
			params.Set("device_id", device.ID)
			return params, nil
		}
	}
	return nil, fmt.Errorf("device %q not found", c.device)
}

func looksLikeDeviceID(value string) bool {
	base := value
	if index := strings.Index(base, "_amzn_"); index >= 0 {
		suffix := base[index+len("_amzn_"):]
		if suffix == "" {
			return false
		}
		for _, char := range suffix {
			if char < '0' || char > '9' {
				return false
			}
		}
		base = base[:index]
	}
	switch len(base) {
	case 40:
		return isHexDeviceID(base, nil)
	case 36:
		return isHexDeviceID(base, map[int]bool{8: true, 13: true, 18: true, 23: true})
	default:
		return false
	}
}

func isHexDeviceID(value string, separators map[int]bool) bool {
	for index, char := range value {
		if separators[index] {
			if char != '-' {
				return false
			}
			continue
		}
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func (c *Client) Queue(ctx context.Context) (Queue, error) {
	var raw queueResponse
	if err := c.get(ctx, "/me/player/queue", nil, &raw); err != nil {
		if errors.Is(err, ErrNoContent) {
			return Queue{}, nil
		}
		return Queue{}, err
	}
	q := Queue{}
	if raw.CurrentlyPlaying.ID != "" {
		item := mapTrack(raw.CurrentlyPlaying)
		q.CurrentlyPlaying = &item
	}
	for _, item := range raw.Queue {
		q.Queue = append(q.Queue, mapTrack(item))
	}
	return q, nil
}
