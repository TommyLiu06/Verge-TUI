package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"verge-tui/internal/controller"
)

const DelayConcurrency = 4

// DelayError is Clash Verge Rev's sentinel for a failed measurement.
const DelayError = 1_000_000

type DelayResult struct {
	Name        string
	Delay       int
	Done, Total int
}

// TestDelay bounds both requests and queued results. Progress is called serially
// as nodes finish, before returning, including for timeouts and failed requests.
func (b *Backend) TestDelay(ctx context.Context, a Action, progress func(DelayResult)) error {
	if b.Remote != nil {
		return b.Remote.Delay(ctx, a, func(data json.RawMessage) error {
			var update DelayResult
			if err := json.Unmarshal(data, &update); err != nil {
				return err
			}
			if progress != nil {
				progress(update)
			}
			return nil
		})
	}
	c, err := b.Store.Read()
	if err != nil {
		return err
	}
	client := controller.New(c)
	defer client.Close()
	snap, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	testURL := a.Value
	if testURL == "" {
		testURL = snap.Proxies[a.ID].TestURL
	}
	if testURL == "" {
		testURL = "http://cp.cloudflare.com/generate_204"
	}
	u, err := url.Parse(testURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("invalid delay test URL")
	}
	var names []string
	if a.Node != "" {
		names = []string{a.Node}
	} else {
		group, ok := snap.Proxies[a.ID]
		if !ok {
			return errors.New("group does not exist")
		}
		seen := make(map[string]bool, len(group.All))
		for _, name := range group.All {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	type measurement struct {
		name  string
		delay int
		err   error
	}
	results := make(chan measurement, DelayConcurrency)
	workers := min(DelayConcurrency, len(names))
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Go(func() {
			for i := worker; i < len(names); i += workers {
				if ctx.Err() != nil {
					return
				}
				delay, err := client.Delay(ctx, names[i], testURL)
				if err != nil {
					delay = DelayError
				}
				select {
				case results <- measurement{names[i], delay, err}:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	go func() { wg.Wait(); close(results) }()
	done, failures := 0, 0
	var singleErr error
	for result := range results {
		if ctx.Err() != nil {
			continue
		}
		done++
		if result.err != nil || result.delay <= 0 {
			failures++
			singleErr = result.err
		}
		if progress != nil {
			progress(DelayResult{result.name, result.delay, done, len(names)})
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a.Node != "" && singleErr != nil {
		return singleErr
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d nodes failed delay testing; successful results were kept", failures, len(names))
	}
	return nil
}
