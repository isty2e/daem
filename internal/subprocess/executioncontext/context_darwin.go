package executioncontext

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/executionidentity"
	"golang.org/x/sys/unix"
)

func readExecutionContext(ctx context.Context) (executionidentity.Context, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, "/usr/sbin/ioreg", "-a", "-r", "-d", "1", "-c", "IOPlatformExpertDevice")
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	var output executionContextOutput
	command.Stdout, command.Stderr = &output, io.Discard
	command.WaitDelay = 250 * time.Millisecond
	if err := command.Run(); err != nil {
		return executionidentity.Context{}, errors.New("platform host identity observation unavailable")
	}
	host, err := darwinExecutionHostID(output.String())
	if err != nil {
		return executionidentity.Context{}, err
	}
	epoch, err := unix.Sysctl("kern.bootsessionuuid")
	if err != nil {
		return executionidentity.Context{}, err
	}
	return executionidentity.NewContextWithVisibility(executionidentity.Darwin,
		executionContextDigest("host", "darwin", host), strings.ToLower(strings.TrimSpace(epoch)),
		executionContextDigest("visibility", "darwin-global-pid-v1"))
}

type executionContextOutput struct{ bytes.Buffer }

func (output *executionContextOutput) Write(value []byte) (int, error) {
	if output.Len()+len(value) > 1<<20 {
		return 0, errors.New("platform identity output exceeds its observation bound")
	}
	return output.Buffer.Write(value)
}

func darwinExecutionHostID(document string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(document))
	var host string
	seen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errors.New("invalid platform identity document")
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var key string
		if err := decoder.DecodeElement(&key, &start); err != nil {
			return "", errors.New("invalid platform identity document")
		}
		if key != "IOPlatformUUID" {
			continue
		}
		if seen {
			return "", errors.New("ambiguous platform host identity")
		}
		seen = true
		for {
			token, err := decoder.Token()
			if err != nil {
				return "", errors.New("invalid platform identity document")
			}
			if start, ok := token.(xml.StartElement); ok {
				if start.Name.Local != "string" || decoder.DecodeElement(&host, &start) != nil {
					return "", errors.New("invalid platform host identity")
				}
				break
			}
			if content, ok := token.(xml.CharData); ok && strings.TrimSpace(string(content)) == "" {
				continue
			}
			return "", errors.New("invalid platform identity document")
		}
	}
	return canonicalExecutionHostID(host)
}
