crony
=====

Crony runs cron-like tasks that modify a git repository.

Usage
-----

    $ crony <url-to-git-repo>

Crony will make a local clone of the repo, and look for a file named `crontab` in it.  It will then start running the commands scheduled in the crontab.  Crony will regularly check for updates to the crontab.

Each command is run with a working directory containing its own copy of the git repo.  Any changes it makes in this directory will be automatically committed and pushed back to the repo.
