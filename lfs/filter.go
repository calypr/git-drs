package lfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
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
	in     *PktScanner
	out    *PktEncoder
	smudge SmudgeFunc
	clean  CleanFunc
	logger *slog.Logger
}

// NewGitFilter creates a GitFilter that reads from in and writes to out.
func NewGitFilter(in io.Reader, out io.Writer, logger *slog.Logger) *GitFilter {
	return &GitFilter{
		in:     NewPktScanner(in),
		out:    NewPktEncoder(out),
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
	// --- version negotation from git ---
	pkt, err := f.in.ReadPacket()
	if err != nil {
		return fmt.Errorf("reading welcome: %w", err)
	}
	if pkt.Type != PktData || strings.TrimSpace(string(pkt.Data)) != "git-filter-client" {
		return fmt.Errorf("expected 'git-filter-client', got %q", pkt.Data)
	}

	pkt, err = f.in.ReadPacket()
	if err != nil {
		return fmt.Errorf("reading version: %w", err)
	}
	if pkt.Type != PktData || !strings.HasPrefix(string(pkt.Data), "version=") {
		return fmt.Errorf("expected 'version=', got %q", pkt.Data)
	}
	if ver := strings.TrimSpace(strings.TrimPrefix(string(pkt.Data), "version=")); ver != "2" {
		return fmt.Errorf("unsupported filter protocol version %q (only version 2 is supported)", ver)
	}

	// consume flush after version block
	if err := f.expectFlush(); err != nil {
		return fmt.Errorf("after version: %w", err)
	}

	// --- send our identity ---
	if err := f.out.WriteString("git-filter-server\n"); err != nil {
		return err
	}
	if err := f.out.WriteString("version=2\n"); err != nil {
		return err
	}
	if err := f.out.WriteFlush(); err != nil {
		return err
	}

	// --- read capabilities from git ---
	if err := f.drainUntilFlush(); err != nil {
		return fmt.Errorf("reading capabilities: %w", err)
	}

	// --- advertise our capabilities ---
	if err := f.out.WriteString("capability=clean\n"); err != nil {
		return err
	}
	if err := f.out.WriteString("capability=smudge\n"); err != nil {
		return err
	}
	return f.out.WriteFlush()
}

// --------------------------------------------------------------------------
// Per-request processing
// --------------------------------------------------------------------------

func (f *GitFilter) processOne(ctx context.Context) error {
	req, err := f.readRequest()
	if err != nil {
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
		if err := f.out.WriteString("status=error\n"); err != nil {
			return err
		}
		if err := f.out.WriteFlush(); err != nil {
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
	if err := f.out.WriteString("status=success\n"); err != nil {
		return err
	}
	if err := f.out.WriteFlush(); err != nil {
		return err
	}

	// Write content in chunks ≤ 65516 bytes each.
	const maxChunk = 65516
	if len(data) == 0 {
		// Empty content: write a single flush to represent empty body.
		if err := f.out.WriteFlush(); err != nil {
			return err
		}
	} else {
		for len(data) > 0 {
			n := len(data)
			if n > maxChunk {
				n = maxChunk
			}
			if err := f.out.WritePacket(data[:n]); err != nil {
				return err
			}
			data = data[n:]
		}
		if err := f.out.WriteFlush(); err != nil {
			return err
		}
	}

	// Second flush signals end-of-command.
	return f.out.WriteFlush()
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// readRequest reads the command header section terminated by a delimiter packet.
// Returns (FilterRequest, nil) on success, or (_, io.EOF) if the stream is done.
func (f *GitFilter) readRequest() (FilterRequest, error) {
	var req FilterRequest
	for {
		pkt, err := f.in.ReadPacket()
		if err != nil {
			return FilterRequest{}, err
		}
		switch pkt.Type {
		case PktFlush:
			// A flush before any data means the stream is done.
			return FilterRequest{}, io.EOF
		case PktDelim:
			// Delimiter marks end of header section.
			return req, nil
		case PktData:
			line := strings.TrimSuffix(string(pkt.Data), "\n")
			if kv := strings.SplitN(line, "=", 2); len(kv) == 2 {
				switch kv[0] {
				case "command":
					req.Command = kv[1]
				case "pathname":
					req.Pathname = kv[1]
				}
			}
		}
	}
}

// readContent reads pkt-line data packets until a flush packet, returning all
// data concatenated. git sends content flush-terminated.
func (f *GitFilter) readContent() ([]byte, error) {
	var buf []byte
	for {
		pkt, err := f.in.ReadPacket()
		if err != nil {
			return nil, err
		}
		switch pkt.Type {
		case PktFlush:
			return buf, nil
		case PktData:
			buf = append(buf, pkt.Data...)
		default:
			return nil, fmt.Errorf("unexpected packet type %d while reading content", pkt.Type)
		}
	}
}

// expectFlush reads the next packet and returns an error if it is not a flush.
func (f *GitFilter) expectFlush() error {
	pkt, err := f.in.ReadPacket()
	if err != nil {
		return err
	}
	if pkt.Type != PktFlush {
		return fmt.Errorf("expected flush packet, got type %d data %q", pkt.Type, pkt.Data)
	}
	return nil
}

// drainUntilFlush discards data packets until a flush is seen.
func (f *GitFilter) drainUntilFlush() error {
	for {
		pkt, err := f.in.ReadPacket()
		if err != nil {
			return err
		}
		if pkt.Type == PktFlush {
			return nil
		}
	}
}
