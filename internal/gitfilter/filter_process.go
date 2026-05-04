package gitfilter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/git-lfs/pktline"
)

// SmudgeFunc is called for each smudge (checkout) request.
// req contains the pathname and command metadata.
// ptr is the LFS pointer payload received from git (may be non-LFS content for untracked files).
// dst is where the real file content must be written.
type SmudgeFunc func(ctx context.Context, req FilterRequest, ptr io.Reader, dst io.Writer) error

// CleanFunc is called for each clean (stage) request.
// req contains the pathname and command metadata.
// content is the full file content received from git.
// dst is where the LFS pointer must be written.
type CleanFunc func(ctx context.Context, req FilterRequest, content io.Reader, dst io.Writer) error

// FilterRequest describes a single filter request from git.
type FilterRequest struct {
	// Command is "smudge" or "clean".
	Command string
	// Pathname is the repo-relative file path being processed.
	Pathname string
}

// GitFilter implements the git long-running filter process protocol v2.
//
// Protocol reference:
//
//	https://git-scm.com/docs/gitattributes#_long_running_filter_process
//
// Usage:
//
//	f := NewGitFilter(os.Stdin, os.Stdout).
//	    OnSmudge(smudgeFn).
//	    OnClean(cleanFn)
//	err := f.Run(ctx)
type GitFilter struct {
	pl     *pktline.Pktline
	out    io.Writer
	smudge SmudgeFunc
	clean  CleanFunc
	logger *slog.Logger
}

// NewGitFilter creates a GitFilter that reads from in and writes to out.
func NewGitFilter(in io.Reader, out io.Writer, logger *slog.Logger) *GitFilter {
	return &GitFilter{
		pl:     pktline.NewPktline(in, out),
		out:    out,
		logger: logger,
	}
}

// OnSmudge registers the smudge handler and returns the receiver for chaining.
func (f *GitFilter) OnSmudge(fn SmudgeFunc) *GitFilter {
	f.smudge = fn
	return f
}

// OnClean registers the clean handler and returns the receiver for chaining.
func (f *GitFilter) OnClean(fn CleanFunc) *GitFilter {
	f.clean = fn
	return f
}

// Run performs the capability handshake and then processes filter requests
// until the underlying reader is exhausted or the context is cancelled.
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

// --------------------------------------------------------------------------
// Handshake
// --------------------------------------------------------------------------

// handshake implements the git filter v2 capability exchange.
//
// git → filter:
//
//	PKT-LINE("git-filter-client\n")
//	PKT-LINE("version=2\n")
//	flush-pkt
//	PKT-LINE("capability=clean\n") + PKT-LINE("capability=smudge\n") + flush-pkt
//
// filter → git:
//
//	PKT-LINE("git-filter-server\n")
//	PKT-LINE("version=2\n")
//	flush-pkt
//	PKT-LINE("capability=clean\n") + PKT-LINE("capability=smudge\n") + flush-pkt
func (f *GitFilter) handshake() error {
	// --- version negotiation from git ---
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

	// --- send our identity ---
	if err := f.pl.WritePacketList([]string{"git-filter-server", "version=2"}); err != nil {
		return err
	}

	// --- read capabilities from git ---
	if _, err := f.pl.ReadPacketList(); err != nil {
		return fmt.Errorf("reading capabilities: %w", err)
	}

	// --- advertise our capabilities ---
	return f.pl.WritePacketList([]string{"capability=clean", "capability=smudge"})
}

// --------------------------------------------------------------------------
// Per-request processing
// --------------------------------------------------------------------------

func (f *GitFilter) processOne(ctx context.Context) error {
	f.logger.Debug("Waiting for next filter request...")
	req, err := f.readRequest()
	if err != nil {
		f.logger.Debug(fmt.Sprintf("Error reading filter request: %v", err))
		return err
	}
	f.logger.Debug("Received filter request", "command", req.Command, "pathname", req.Pathname)
	// Read content (between the delimiter and the trailing flush).
	content, err := f.readContent()
	if err != nil {
		return fmt.Errorf("reading content for %s %s: %w", req.Command, req.Pathname, err)
	}

	var handlerErr error
	switch req.Command {
	case "smudge":
		handlerErr = f.handleSmudge(ctx, req, content)
	case "clean":
		handlerErr = f.handleClean(ctx, req, content)
	default:
		// Unknown command: respond with error status and empty content.
		handlerErr = fmt.Errorf("unknown command %q", req.Command)
	}

	if handlerErr != nil {
		// Send error status; git will use the pointer/raw bytes itself.
		if err := f.pl.WritePacketList([]string{"status=error"}); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (f *GitFilter) handleSmudge(ctx context.Context, req FilterRequest, content []byte) error {
	if f.smudge == nil {
		return f.passthroughSmudge(content)
	}

	var dst bytes.Buffer
	if err := f.smudge(ctx, req, bytes.NewReader(content), &dst); err != nil {
		return err
	}
	return f.writeSuccessResponse(dst.Bytes())
}

func (f *GitFilter) handleClean(ctx context.Context, req FilterRequest, content []byte) error {
	if f.clean == nil {
		return f.passthroughClean(content)
	}

	var dst bytes.Buffer
	if err := f.clean(ctx, req, bytes.NewReader(content), &dst); err != nil {
		return err
	}
	return f.writeSuccessResponse(dst.Bytes())
}

// passthroughSmudge sends content as-is (smudge no-op).
func (f *GitFilter) passthroughSmudge(content []byte) error {
	return f.writeSuccessResponse(content)
}

// passthroughClean sends content as-is (clean no-op).
func (f *GitFilter) passthroughClean(content []byte) error {
	return f.writeSuccessResponse(content)
}

// writeSuccessResponse writes the success response including pkt-line framed content.
//
// Format:
//
//	PKT-LINE("status=success\n")
//	flush-pkt
//	[PKT-LINE(content chunks)]
//	flush-pkt
//	flush-pkt  (second flush signals end of command)
func (f *GitFilter) writeSuccessResponse(data []byte) error {
	if err := f.pl.WritePacketList([]string{"status=success"}); err != nil {
		return err
	}

	w := pktline.NewPktlineWriter(f.out, pktline.MaxPacketLength)
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	// Send trailing key=value list (empty) terminated with flush.
	return f.pl.WritePacketList(nil)
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// readRequest reads the command header section terminated by a delimiter packet.
// Returns (FilterRequest, nil) on success, or (_, io.EOF) if the stream is done.
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

// readContent reads pkt-line data packets until a flush packet, returning all
// data concatenated. git sends content flush-terminated.
func (f *GitFilter) readContent() ([]byte, error) {
	return io.ReadAll(pktline.NewPktlineReaderFromPktline(f.pl, pktline.MaxPacketLength))
}
