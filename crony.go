package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path"
	"time"

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
		log.Printf("couldn't pull; was origin's history rewritten?")
		log.Printf("overwriting local head with origin's...")
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
	log.Printf("Got crontab:\n%s", string(contents))
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
			log.Printf("error pulling crontab for %s: %s", repo.name, err)
		}
		for _ = range ticker.C {
			if err := pullCrontab(repo, crontabUpdates); err != nil {
				log.Printf("error pulling crontab for %s: %s", repo.name, err)
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
			stopTime := make(chan time.Time, 1)
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
				log.Printf("Command overran after %s: %s", start.Sub(now), entry.Command)
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
	log.Printf("running: %s", command)
	w, err := repo.Branch()
	if err != nil {
		log.Printf("unable to create branch: %s", err)
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
			log.Printf("unable to write to .fail: %s", err)
		}
	}

	if err := w.Commit(commitMsg); err != nil {
		log.Printf("nothing to commit after running %s", command)
	}

	if err := repo.master.Merge(w); err != nil {
		log.Printf("unable to merge temp branch into local master: %s", err)
		return
	}

	if err := repo.master.Push(); err != nil {
		log.Printf("unable to push master: %s", err)
		log.Printf("trying to overwrite local head with origin for future commits to be rebased on...")
		if err := repo.master.FetchHead(); err != nil {
			log.Printf("error overwriting local head with origin: %s", err)
		}
	}
}

func main() {
	flag.Parse()
	for _, url := range flag.Args() {
		r, err := NewClone(url, url)
		if err != nil {
			log.Fatalf("Error cloning %s: %s", url, err)
		}
		defer r.Close()
		crontabUpdates := watchCrontab(r)
		go executeCrontab(r, crontabUpdates)
	}
	select {}
}
