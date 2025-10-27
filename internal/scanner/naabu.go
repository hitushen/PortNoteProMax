package scanner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/projectdiscovery/goflags"
	portpkg "github.com/projectdiscovery/naabu/v2/pkg/port"
	"github.com/projectdiscovery/naabu/v2/pkg/result"
	"github.com/projectdiscovery/naabu/v2/pkg/runner"

	"github.com/hitushen/portnotepro/internal/targets"
)

func runNaabu(ctx context.Context, address string, ports []int) (map[int]*portpkg.Port, error) {
	openPorts := make(map[int]*portpkg.Port)

	targetsList := targets.Build(address)
	if len(targetsList) == 0 {
		return nil, fmt.Errorf("invalid target address: %q", address)
	}

	onResult := func(hr *result.HostResult) {
		if hr == nil {
			return
		}
		for _, p := range hr.Ports {
			if p == nil {
				continue
			}
			openPorts[p.Port] = p
		}
	}

	opts := runner.Options{
		Host:             goflags.StringSlice(targetsList),
		ScanType:         "c",
		OnResult:         onResult,
		JSON:             false,
		NoColor:          true,
		Verbose:          false,
		Stdin:            false,
		Stream:           true,
		Ports:            "1-65535",
		Retries:          1,
		Rate:             3000,
		Timeout:          5000 * time.Millisecond,
		ServiceDiscovery: true,
	}

	if len(ports) > 0 {
		str := make([]string, len(ports))
		for i, p := range ports {
			str[i] = strconv.Itoa(p)
		}
		opts.Ports = strings.Join(str, ",")
	}

	r, err := runner.NewRunner(&opts)
	if err != nil {
		return nil, fmt.Errorf("naabu runner init: %w", err)
	}
	defer r.Close()

	if err := r.RunEnumeration(ctx); err != nil {
		return nil, fmt.Errorf("naabu enumeration: %w", err)
	}

	return openPorts, nil
}
