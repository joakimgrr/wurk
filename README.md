# wurk

A git worktree per task, with the `cd` included.

```console
$ wurk PROJ-2222-work-on-login-system
✓ created PROJ-2222-work-on-login-system from origin/main at ~/dev/myrepo-worktrees/PROJ-2222-work-on-login-system
$ pwd
/Users/you/dev/myrepo-worktrees/PROJ-2222-work-on-login-system
```

One name gets you a branch, a worktree to hold it, and a shell already sitting
in it. When the work has landed, one command takes all of it away again.

## Install

```sh
go install github.com/joakimgrr/wurk@latest
echo 'eval "$(wurk shell-init zsh)"' >> ~/.zshrc   # or bash, or fish
```

The second line is what makes the `cd` happen: a process cannot change its
parent shell's directory, so wurk prints the path and a small shell function
follows it. Without it, `cd "$(wurk some-name)"` does the same by hand.

## Commands

| command | what it does |
| --- | --- |
| `wurk <name>` | create the branch and worktree, and move into it |
| `wurk list` | what exists and how it stands |
| `wurk rm <name>` | remove the worktree and delete the branch |
| `wurk config` | show the settings; `--init` writes a starter file |

`<name>` is both the branch and the directory, so `feature/login` becomes a
branch of that name in a `feature-login` directory. New branches start from
`origin/HEAD`, or `--base` says otherwise. Asking twice for the same name just
takes you back to it.

```console
$ wurk list
   BRANCH                          CHANGES    VS ORIGIN/MAIN  STATE          PATH
────────────────────────────────────────────────────────────────────────────────────────────────────────────
●  main                            clean      up to date      main worktree  ~/dev/myrepo
   merged-work                     clean      ↓2              merged         merged-work
   squashed-work                   clean      ↑2 ↓3           squash-merged  squashed-work
   PROJ-2222-work-on-login-system  2 changes  ↑1 ↓3           not merged     PROJ-2222-work-on-login-system
   paths relative to ~/dev/myrepo-worktrees
```

`STATE` is the column `wurk rm` reads. It refuses a branch whose work has not
landed, and a worktree with uncommitted changes, until `--force` says to throw
them away. A **squash-merged** branch counts as landed — wurk rebuilds the
commit a squash would have produced and asks git whether that content is
already in the base, so a squash-merging repository does not look like a wall
of unmerged branches.

## Configuration

Optional, at `~/.config/wurk/config.toml`. `wurk config --init` writes a
commented starter.

```toml
# Absolute, ~-relative, or relative to the repository root.
worktree_dir = "~/dev/worktrees"
```

Worktrees default to a sibling of the repository, so `~/dev/myrepo` gets
`~/dev/myrepo-worktrees/<name>`. The config file overrides that,
`WURK_WORKTREE_DIR` overrides the file, and `--dir` overrides everything;
`wurk config` shows which one won.

## Development

`go test ./...` drives real `git` against throwaway repositories, covering
ordinary merges, squash merges, unmerged work and dirty worktrees.

Built with [cobra](https://github.com/spf13/cobra) and
[lipgloss](https://github.com/charmbracelet/lipgloss).
