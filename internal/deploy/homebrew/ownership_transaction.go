//go:build linux

package homebrew

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"
)

// This unexported engine has no production caller. Its injected adapter is supplied
// only by fixture tests; it does not call chown or change actual object modes.
var errOwnershipFixture = errors.New("modeled ownership fixture refused")

type ownershipState struct {
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
	Mode uint32 `json:"mode"`
}
type ownershipObject struct {
	Kind     string         `json:"kind"`
	Identity Identity       `json:"identity"`
	State    ownershipState `json:"state"`
}
type ownershipEntry struct {
	Path     string         `json:"path"`
	Kind     string         `json:"kind"`
	Identity Identity       `json:"identity"`
	Before   ownershipState `json:"before"`
	After    ownershipState `json:"after"`
}
type ownershipAdapter interface {
	observe(string) (ownershipObject, error)
	mutate(string, ownershipState, ownershipState) error
}
type ownershipFixtureTransaction struct {
	root, journal string
	entries       []ownershipEntry
	adapter       ownershipAdapter
}
type ownershipFixtureRecord struct {
	Version   int              `json:"version"`
	Root      Identity         `json:"root"`
	Entries   []ownershipEntry `json:"entries"`
	Direction string           `json:"direction"`
	Phase     string           `json:"phase"`
	Progress  []bool           `json:"progress"`
}
type ownershipFixtureHook func(string, int) error

// inventory checks actual filesystem identity and exact containment, without
// following symlinks. The adapter supplies only simulated UID/GID/mode state.
func (t ownershipFixtureTransaction) inventory() error {
	if t.adapter == nil || len(t.entries) != 3 || !filepath.IsAbs(t.root) || !filepath.IsAbs(t.journal) || filepath.Dir(t.journal) == t.root {
		return errOwnershipFixture
	}
	rootInfo, err := os.Lstat(t.root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errOwnershipFixture
	}
	journalDir, err := os.Lstat(filepath.Dir(t.journal))
	if err != nil || !journalDir.IsDir() || journalDir.Mode()&os.ModeSymlink != 0 || journalDir.Mode().Perm()&0077 != 0 {
		return errOwnershipFixture
	}
	names := []string{".", "empty", "file"}
	kinds := []string{"directory", "directory", "file"}
	for i, e := range t.entries {
		if e.Path != names[i] || e.Kind != kinds[i] || e.Identity.Device == 0 || e.Identity.Inode == 0 || e.Before.Mode > 0777 || e.After.Mode > 0777 || e.After.Mode&^e.Before.Mode != 0 {
			return errOwnershipFixture
		}
	}
	var found []string
	err = filepath.WalkDir(t.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errOwnershipFixture
		}
		rel, e := filepath.Rel(t.root, path)
		if e != nil {
			return errOwnershipFixture
		}
		if d.Type()&os.ModeSymlink != 0 || !d.IsDir() && !d.Type().IsRegular() {
			return errOwnershipFixture
		}
		found = append(found, rel)
		if len(found) > 3 {
			return errOwnershipFixture
		}
		return nil
	})
	if err != nil {
		return errOwnershipFixture
	}
	sort.Strings(found)
	if !reflect.DeepEqual(found, names) {
		return errOwnershipFixture
	}
	for _, e := range t.entries {
		info, err := os.Lstat(filepath.Join(t.root, e.Path))
		if err != nil {
			return errOwnershipFixture
		}
		id, _, _ := metadata(info)
		if e.Kind == "file" {
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || st.Nlink != 1 {
				return errOwnershipFixture
			}
		}
		if id != e.Identity || (e.Kind == "directory") != info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errOwnershipFixture
		}
		obj, err := t.adapter.observe(e.Path)
		if err != nil || obj.Identity != e.Identity || obj.Kind != e.Kind {
			return errOwnershipFixture
		}
	}
	return nil
}

func (t ownershipFixtureTransaction) inspect(r ownershipFixtureRecord) error {
	if err := t.inventory(); err != nil {
		return err
	}
	current := 0
	for current < len(r.Progress) && r.Progress[current] {
		current++
	}
	for i, e := range t.entries {
		obj, err := t.adapter.observe(e.Path)
		if err != nil {
			return errOwnershipFixture
		}
		pre, post := e.Before, e.After
		if r.Direction == "inverse" {
			pre, post = post, pre
		}
		// Only the current in-flight mutation can precede its progress record.
		if r.Progress[i] || r.Phase == "done" {
			if obj.State != post {
				return errOwnershipFixture
			}
		} else if obj.State != pre && !(i == current && obj.State == post) {
			return errOwnershipFixture
		}
	}
	return nil
}

func (t ownershipFixtureTransaction) record(r ownershipFixtureRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return errOwnershipFixture
	}
	b = append(b, '\n')
	// A lingering temp is ambiguous: never overwrite or erase recovery evidence.
	f, err := os.OpenFile(t.journal+".new", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errOwnershipFixture
	}
	if n, err := f.Write(b); err != nil || n != len(b) {
		f.Close()
		return errOwnershipFixture
	}
	if f.Sync() != nil || f.Close() != nil {
		return errOwnershipFixture
	}
	if os.Rename(t.journal+".new", t.journal) != nil {
		return errOwnershipFixture
	}
	dir, err := os.Open(filepath.Dir(t.journal))
	if err != nil {
		return errOwnershipFixture
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return errOwnershipFixture
	}
	return nil
}

func (t ownershipFixtureTransaction) read() (ownershipFixtureRecord, bool, error) {
	var r ownershipFixtureRecord
	// A temp might contain newer progress or incomplete bytes. Refuse it rather
	// than infer which record was durable across a failed replacement/sync.
	if _, err := os.Lstat(t.journal + ".new"); err == nil || !errors.Is(err, os.ErrNotExist) {
		return r, false, errOwnershipFixture
	}
	info, statErr := os.Lstat(t.journal)
	if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return r, false, errOwnershipFixture
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return r, false, errOwnershipFixture
	}
	f, err := os.Open(t.journal)
	if errors.Is(err, os.ErrNotExist) {
		return r, false, nil
	}
	if err != nil {
		return r, false, errOwnershipFixture
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 || stat.Size() < 2 || stat.Size() > 8192 {
		return r, false, errOwnershipFixture
	}
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(data) > 8192 || data[len(data)-1] != '\n' {
		return r, false, errOwnershipFixture
	}
	if json.Unmarshal(data[:len(data)-1], &r) != nil {
		return r, false, errOwnershipFixture
	}
	canonical, _ := json.Marshal(r)
	if string(canonical) != string(data[:len(data)-1]) || r.Version != 1 || len(r.Progress) != 3 || !reflect.DeepEqual(r.Entries, t.entries) || r.Root != t.entries[0].Identity || (r.Direction != "apply" && r.Direction != "inverse") || (r.Phase != "pending" && r.Phase != "done") {
		return r, false, errOwnershipFixture
	}
	seenPending := false
	for _, p := range r.Progress {
		if !p {
			seenPending = true
		} else if seenPending {
			return r, false, errOwnershipFixture
		}
	}
	if r.Phase == "done" && seenPending {
		return r, false, errOwnershipFixture
	}
	return r, true, nil
}

// run accepts only exact before/post modeled state on the original three inodes.
// A pending inverse is recoverable; starting one requires a completed apply.
func (t ownershipFixtureTransaction) run(inverse bool, hook ownershipFixtureHook) error {
	if err := t.inventory(); err != nil {
		return err
	}
	step := func(s string, i int) error {
		if hook != nil {
			return hook(s, i)
		}
		return nil
	}
	old, exists, err := t.read()
	if err != nil {
		return err
	}
	direction := "apply"
	if inverse {
		direction = "inverse"
	}
	if exists && old.Direction != direction && old.Phase != "done" {
		return errOwnershipFixture
	}
	if inverse && !exists {
		return errOwnershipFixture
	}
	if exists && old.Direction == direction {
		if err := t.inspect(old); err != nil {
			return err
		}
		if old.Phase == "done" {
			return nil
		}
	} else {
		if exists && t.inspect(old) != nil {
			return errOwnershipFixture
		}
		if err := step("intent", 0); err != nil {
			return err
		}
		old = ownershipFixtureRecord{Version: 1, Root: t.entries[0].Identity, Entries: t.entries, Direction: direction, Phase: "pending", Progress: make([]bool, 3)}
		// A new intent cannot adopt somebody else's partially transitioned tree.
		for _, e := range t.entries {
			state, err := t.adapter.observe(e.Path)
			if err != nil {
				return errOwnershipFixture
			}
			expected := e.Before
			if inverse {
				expected = e.After
			}
			if state.State != expected {
				return errOwnershipFixture
			}
		}
		if err := t.record(old); err != nil {
			return err
		}
	}
	for i, e := range t.entries {
		if err := t.inspect(old); err != nil {
			return err
		}
		pre, post := e.Before, e.After
		if inverse {
			pre, post = post, pre
		}
		if err := step("before", i); err != nil {
			return err
		}
		obj, err := t.adapter.observe(e.Path)
		if err != nil {
			return errOwnershipFixture
		}
		if obj.State == pre {
			if old.Progress[i] {
				return errOwnershipFixture
			}
			if err := t.adapter.mutate(e.Path, pre, post); err != nil {
				return errOwnershipFixture
			}
		}
		if err := step("mutation", i); err != nil {
			return err
		}
		if err := t.inspect(old); err != nil {
			return err
		}
		if !old.Progress[i] {
			old.Progress[i] = true
			if err := t.record(old); err != nil {
				return err
			}
		}
		if err := step("progress", i); err != nil {
			return err
		}
	}
	if err := t.inspect(old); err != nil {
		return err
	}
	if err := step("done", 0); err != nil {
		return err
	}
	old.Phase = "done"
	if err := t.record(old); err != nil {
		return err
	}
	return nil
}
