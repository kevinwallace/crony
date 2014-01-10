package main

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/golang/glog"
)

func randomStr(alphabet string, n int) string {
	var s string
	for i := 0; i < n; i++ {
		s += string(alphabet[rand.Intn(len(alphabet))])
	}
	return s
}

func tempDir() string {
	var path string
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for {
		suffix := randomStr(chars, 16)
		path = os.TempDir() + "/crony." + suffix
		err := os.Mkdir(path, 0700)
		if err == nil {
			break
		} else if os.IsExist(err) {
			continue
		} else {
			panic(err)
		}
	}
	return path
}

func cp(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

type repo struct {
	name           string
	master         *workdir
	mu             sync.Mutex
	lastTempBranch int
}

// NewClone creates a local clone of a remote repo.
func NewClone(name string, origin string) (*repo, error) {
	r := &repo{
		name: name,
		master: &workdir{
			branch: "master",
			dir:    tempDir(),
		},
	}
	r.master.repo = r
	if err := r.master.git("clone", origin, r.master.dir); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *repo) tempBranchName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := fmt.Sprintf("temp%d", r.lastTempBranch)
	r.lastTempBranch++
	return name
}

// Branch creates a new temporary branch off of master, and a new workdir with that branch checked out.
func (r *repo) Branch() (*workdir, error) {
	w := &workdir{
		repo:   r,
		branch: r.tempBranchName(),
		dir:    tempDir(),
	}
	oldGitDir := path.Join(r.master.dir, ".git")
	newGitDir := path.Join(w.dir, ".git")
	if err := os.Mkdir(newGitDir, 0700); err != nil {
		return nil, err
	}
	gitFiles := []string{"config", "refs", "logs/refs", "objects", "info", "hooks", "packed-refs", "remotes", "rr-cache", "svn"}
	for _, file := range gitFiles {
		oldPath := path.Join(oldGitDir, file)
		newPath := path.Join(newGitDir, file)
		if err := os.MkdirAll(path.Dir(newPath), 0700); err != nil {
			return nil, err
		}
		if err := os.Symlink(oldPath, newPath); err != nil {
			return nil, err
		}
	}

	m := r.master
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := cp(path.Join(oldGitDir, "HEAD"), path.Join(newGitDir, "HEAD")); err != nil {
		return nil, err
	}

	if err := m.git("branch", w.branch); err != nil {
		return nil, err
	}
	if err := w.git("checkout", "-f", w.branch); err != nil {
		return nil, err
	}
	return w, nil
}

func (r *repo) Close() error {
	return r.master.Close()
}

type workdir struct {
	repo   *repo
	mu     sync.Mutex
	branch string
	dir    string
}

func (w *workdir) git(args ...string) error {
	_, err := w.gitOutput(args...)
	return err
}

func (w *workdir) gitOutput(args ...string) ([]byte, error) {
	glog.V(3).Infof("%s$ git %s", w.branch, strings.Join(args, " "))
	cmd := exec.Command("git", args...)
	cmd.Dir = w.dir
	output, err := cmd.CombinedOutput()
	glog.V(4).Infof(string(output))
	if err != nil {
		return output, fmt.Errorf("%s\n%s", output, err)
	}
	return output, nil
}

// Pull latest changes from origin, and rebase any local changes on top of origin's head.
// If that's not possible, return an error, leaving the workdir as it was.
func (w *workdir) Pull() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pull()
}

// FetchHead fetches the current branch from origin, overwrites the local HEAD with origin's HEAD,
// and resets the local workdir to HEAD.
// This will drop any local changes, committed or not!
func (w *workdir) FetchHead() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.git("fetch", "origin", w.branch); err != nil {
		return err
	}
	if err := w.git("reset", "--hard", "FETCH_HEAD"); err != nil {
		return err
	}
	if err := w.git("reset", "-df"); err != nil {
		return err
	}
	return nil
}

func (w *workdir) pull() error {
	if err := w.git("pull", "--rebase"); err != nil {
		w.git("rebase", "--abort")
		return err
	}
	return nil
}

// HasChanges determines whether this workdir has un-committed changes, staged or not.
func (w *workdir) HasChanges() (bool, error) {
	output, err := w.gitOutput("status", "-s")
	if err != nil {
		return false, err
	}
	return len(output) > 0, nil
}

func (w *workdir) Commit(msg string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.git("add", "."); err != nil {
		return err
	}
	return w.git("commit", "-a", "-m", msg)
}

func (w *workdir) Merge(other *workdir) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	other.mu.Lock()
	defer other.mu.Unlock()

	if err := other.git("rebase", w.branch); err != nil {
		return err
	}
	if err := w.git("merge", "--ff-only", other.branch); err != nil {
		return err
	}
	return nil
}

func (w *workdir) Push() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.git("push"); err != nil {
		if err := w.pull(); err != nil {
			return err
		}
		return w.git("push")
	}
	return nil
}

func (w *workdir) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.repo.master != w {
		if err := w.repo.master.git("branch", "-D", w.branch); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(w.dir); err != nil {
		return err
	}
	return nil
}
