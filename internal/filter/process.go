package filter

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/git-lfs/pktline"
)

type SmudgeFunc func(ctx context.Context, req FilterRequest, ptr io.Reader, dst io.Writer) error

type CleanFunc func(ctx context.Context, req FilterRequest, content io.Reader, dst io.Writer) error

type FilterRequest struct {
	Command  string
	Pathname string
}

type GitFilter struct {
	pl     *pktline.Pktline
	out    io.Writer
	smudge SmudgeFunc
	clean  CleanFunc
	logger *slog.Logger
}

func NewGitFilter(in io.Reader, out io.Writer, logger *slog.Logger) *GitFilter {
	return &GitFilter{
		pl:     pktline.NewPktline(in, out),
		out:    out,
		logger: logger,
	}
}

func (f *GitFilter) OnSmudge(fn SmudgeFunc) *GitFilter {
	f.smudge = fn
	return f
}

func (f *GitFilter) OnClean(fn CleanFunc) *GitFilter {
	f.clean = fn
	return f
}

func (f *GitFilter) Run(ctx context.Context) error {
	f.logger.Debug("Starting git filter process")
	if err := f.handshake(); err != nil {
		f.logger.Debug(fmt.Sprintf("Handshake failed: %v", err))
		return fmt.Errorf("git-filter: handshake: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f.processOne(ctx); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (f *GitFilter) handshake() error {
	initMsg, err := f.pl.ReadPacketText()
	if err != nil {
		return fmt.Errorf("reading welcome: %w", err)
	}
	if initMsg != "git-filter-client" {
		return fmt.Errorf("expected 'git-filter-client', got %q", initMsg)
	}

	versions, err := f.pl.ReadPacketList()
	if err != nil {
		return fmt.Errorf("reading versions: %w", err)
	}
	if !slices.Contains(versions, "version=2") {
		return fmt.Errorf("unsupported filter protocol versions %v (requires version=2)", versions)
	}

	if err := f.pl.WritePacketList([]string{"git-filter-server", "version=2"}); err != nil {
		return err
	}

	if _, err := f.pl.ReadPacketList(); err != nil {
		return fmt.Errorf("reading capabilities: %w", err)
	}

	return f.pl.WritePacketList([]string{"capability=clean", "capability=smudge"})
}

func (f *GitFilter) processOne(ctx context.Context) error {
	f.logger.Debug("Waiting for next filter request...")
	req, err := f.readRequest()
	if err != nil {
		f.logger.Debug(fmt.Sprintf("Error reading filter request: %v", err))
		return err
	}
	f.logger.Debug("Received filter request", "command", req.Command, "pathname", req.Pathname)
	content := pktline.NewPktlineReaderFromPktline(f.pl, pktline.MaxPacketLength)
	response, err := os.CreateTemp("", "git-drs-filter-response-*")
	if err != nil {
		return fmt.Errorf("creating response spool for %s %s: %w", req.Command, req.Pathname, err)
	}
	defer func() {
		_ = response.Close()
		_ = os.Remove(response.Name())
	}()

	var handlerErr error
	switch req.Command {
	case "smudge":
		handlerErr = f.handleSmudge(ctx, req, content, response)
	case "clean":
		handlerErr = f.handleClean(ctx, req, content, response)
	default:
		handlerErr = fmt.Errorf("unknown command %q", req.Command)
	}
	if _, err := io.Copy(io.Discard, content); err != nil {
		return fmt.Errorf("reading content for %s %s: %w", req.Command, req.Pathname, err)
	}

	if handlerErr != nil {
		if err := f.pl.WritePacketList([]string{"status=error"}); err != nil {
			return err
		}
		return nil
	}
	if _, err := response.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewinding response spool for %s %s: %w", req.Command, req.Pathname, err)
	}
	return f.writeSuccessResponse(response)
}

func (f *GitFilter) handleSmudge(ctx context.Context, req FilterRequest, content io.Reader, dst io.Writer) error {
	if f.smudge == nil {
		_, err := io.Copy(dst, content)
		return err
	}
	return f.smudge(ctx, req, content, dst)
}

func (f *GitFilter) handleClean(ctx context.Context, req FilterRequest, content io.Reader, dst io.Writer) error {
	if f.clean == nil {
		_, err := io.Copy(dst, content)
		return err
	}
	return f.clean(ctx, req, content, dst)
}

func (f *GitFilter) writeSuccessResponse(data io.Reader) error {
	if err := f.pl.WritePacketList([]string{"status=success"}); err != nil {
		return err
	}

	w := pktline.NewPktlineWriter(f.out, pktline.MaxPacketLength)
	if _, err := io.Copy(w, data); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}

	return f.pl.WritePacketList(nil)
}

func (f *GitFilter) readRequest() (FilterRequest, error) {
	requestList, err := f.pl.ReadPacketList()
	if err != nil {
		return FilterRequest{}, err
	}

	var req FilterRequest
	for _, line := range requestList {
		if kv := strings.SplitN(line, "=", 2); len(kv) == 2 {
			switch kv[0] {
			case "command":
				req.Command = kv[1]
			case "pathname":
				req.Pathname = kv[1]
			}
		}
	}
	return req, nil
}
