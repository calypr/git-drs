package prepush

import (
	"bufio"
	"io"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/drsdelete"
)

type pushedRef struct {
	LocalRef  string
	LocalSHA  string
	RemoteRef string
	RemoteSHA string
}

// readPushedRefs parses git's pre-push stdin format and rewinds the reader
// before returning so callers can reuse the buffered input.
func readPushedRefs(f io.ReadSeeker) ([]pushedRef, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	refs := make([]pushedRef, 0)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		refs = append(refs, pushedRef{
			LocalRef:  fields[0],
			LocalSHA:  fields[1],
			RemoteRef: fields[2],
			RemoteSHA: fields[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return refs, nil
}

func branchesFromRefs(refs []pushedRef) []string {
	const prefix = "refs/heads/"
	set := make(map[string]struct{})
	for _, ref := range refs {
		if strings.HasPrefix(ref.LocalRef, prefix) {
			branch := strings.TrimPrefix(ref.LocalRef, prefix)
			if branch != "" {
				set[branch] = struct{}{}
			}
		}
	}
	branches := make([]string, 0, len(set))
	for branch := range set {
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	return branches
}

func drsDeleteRefs(refs []pushedRef) []drsdelete.RefUpdate {
	out := make([]drsdelete.RefUpdate, 0, len(refs))
	for _, ref := range refs {
		out = append(out, drsdelete.RefUpdate{
			OldSHA: strings.TrimSpace(ref.RemoteSHA),
			NewSHA: strings.TrimSpace(ref.LocalSHA),
		})
	}
	return out
}
