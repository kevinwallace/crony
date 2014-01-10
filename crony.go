package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path"
	"time"

	"github.com/golang/glog"
	"github.com/kevinwallace/crontab"
)

var (
	pullFrequency = flag.Duration("pull_frequency", 5*time.Minute,
		"Rate at which to check for upstream changes to the crontab")
)

// Pull latest commit from repo's origin, then parse its crontab and return it on the passed channel.
func pullCrontab(repo *repo, crontabUpdates chan<- []crontab.Entry) error {
	m := repo.master
	if err := m.Pull(); err != nil {
		glog.Warningf("couldn't pull %s; was origin's history rewritten?", repo.name)
		glog.Warningf("overwriting local head with origin's...")
		if err := m.FetchHead(); err != nil {
			return err
		}
	}
	contents, err := ioutil.ReadFile(path.Join(m.dir, "crontab"))
	if err != nil {
		return err
	}
	entries, err := crontab.ParseCrontab(string(contents))
	if err != nil {
		return err
	}
	glog.V(2).Infof("Got crontab:\n%s", string(contents))
	crontabUpdates <- entries
	return nil
}

// Spin up a background goroutine to periodically pull the latest crontab,
// sending it over the returned channel after each check.
func watchCrontab(repo *repo) <-chan []crontab.Entry {
	crontabUpdates := make(chan []crontab.Entry)
	go func() {
		ticker := time.NewTicker(*pullFrequency)
		defer ticker.Stop()
		if err := pullCrontab(repo, crontabUpdates); err != nil {
			glog.Errorf("error pulling crontab for %s: %s", repo.name, err)
		}
		for _ = range ticker.C {
			if err := pullCrontab(repo, crontabUpdates); err != nil {
				glog.Errorf("error pulling crontab for %s: %s", repo.name, err)
			}
		}
	}()
	return crontabUpdates
}

// Handle the incoming stream of parsed crontabs,
// keeping the correct set of executeEntry worker goroutines running.
func executeCrontab(repo *repo, crontabUpdates <-chan []crontab.Entry) {
	var stopTime chan time.Time
	for {
		select {
		case entries := <-crontabUpdates:
			now := time.Now()
			if stopTime != nil {
				stopTime <- now
			}
			stopTime = make(chan time.Time, 1)
			for _, entry := range entries {
				go executeEntry(entry, repo, now, stopTime)
			}
		}
	}
}

// Periodically execute a single crontab entry,
// When a time is sent over the stopTime chan, stop execution at that time and return.
func executeEntry(entry crontab.Entry, repo *repo, now time.Time, stopTime chan time.Time) {
	for {
		next := entry.Schedule.Next(now)
		select {
		case <-time.After(next.Sub(time.Now())):
			start := next
			executeCommand(entry.Command, repo)
			now = time.Now()
			next = entry.Schedule.Next(next)
			if !now.Before(next) {
				glog.Errorf("Command overran after %s: %s", now.Sub(start), entry.Command)
				next = entry.Schedule.Next(now)
			}
		case t := <-stopTime:
			stopTime <- t
			if !t.Before(next) {
				executeCommand(entry.Command, repo)
			}
			return
		}
	}
}

// Execute a single run of a single crontab entry.
// Creates a new branch and workdir off of repo, then executes the given command in that workdir.
// Commits and attempts to push the changes upstream.
func executeCommand(command string, repo *repo) {
	glog.Infof("running: %s", command)
	w, err := repo.Branch()
	if err != nil {
		glog.Errorf("unable to create branch: %s", err)
		return
	}
	defer w.Close()

	cmd := exec.Command("/bin/bash", "-c", command)
	cmd.Dir = w.dir
	out, err := cmd.CombinedOutput()

	ts := time.Now().Format(time.UnixDate)
	commitMsg := fmt.Sprintf("$ %s\n%s", command, string(out))
	if err != nil {
		commitMsg += "\n" + err.Error()
		if err := ioutil.WriteFile(path.Join(w.dir, ".fail"), []byte(ts), 0700); err != nil {
			glog.Errorf("unable to write to .fail: %s", err)
		}
	}

	hasChanges, err := w.HasChanges()
	if err != nil {
		glog.Errorf("couldn't determine whether %s has changes: %s", w.branch, err)
		return
	}
	if !hasChanges {
		glog.Infof("nothing to commit after running: %s", command)
		return
	}

	if err := w.Commit(commitMsg); err != nil {
		glog.Errorf("unable to commit: %s", err)
		return
	}

	if err := repo.master.Merge(w); err != nil {
		glog.Errorf("unable to merge temp branch into local master: %s", err)
		return
	}

	if err := repo.master.Push(); err != nil {
		glog.Errorf("unable to push master: %s", err)
		glog.Errorf("trying to overwrite local head with origin for future commits to be rebased on...")
		if err := repo.master.FetchHead(); err != nil {
			glog.Errorf("error overwriting local head with origin: %s", err)
		}
	}

	glog.Infof("committed changes: %s", command)
}

func main() {
	flag.Parse()
	for _, url := range flag.Args() {
		r, err := NewClone(url, url)
		if err != nil {
			glog.Fatalf("Error cloning %s: %s", url, err)
		}
		defer r.Close()
		crontabUpdates := watchCrontab(r)
		go executeCrontab(r, crontabUpdates)
	}
	select {}
}
