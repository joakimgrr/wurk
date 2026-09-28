<br><br>
<br><br>

<p align="center">
  <img src="WURK.svg" alt="wurk" width="300">
</p>
<br><br>
<br><br>

CLI tool that helps you manage git worktrees and multitasking

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

| command             | what it does                                            |
| ------------------- | ------------------------------------------------------- |
| `wurk <name>`       | create the branch and worktree, and move into it        |
| `wurk list`         | what exists and how it stands                           |
| `wurk done [name]`  | finish with a worktree: remove it and delete its branch |
| `wurk tidy`         | sweep up the worktrees whose work has landed            |
| `wurk setup [name]` | re-run this repository's setup on a worktree            |
| `wurk config`       | show the settings; `--init` writes a starter file       |

`<name>` is both the branch and the directory, so `feature/login` becomes a
branch of that name in a `feature-login` directory. New branches start from
`origin/HEAD`, or `--base` says otherwise. Asking twice for the same name just
takes you back to it.

New branches deliberately do not track the branch they came from, so that a
stray `git push` can never land your commits on `main`. Push the first time
with `git push -u origin HEAD`, or set `git config --global push.autoSetupRemote true`
once and plain `git push` will create the remote branch for you.

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

`STATE` is the column `wurk done` reads: `new` for a branch with no commits of
its own yet, then `merged`, `squash-merged` or `not merged`. A
**squash-merged** branch counts as landed — wurk rebuilds the commit a squash
would have produced and asks git whether that content is already in the base,
so a squash-merging repository does not look like a wall of unmerged branches.

### Finishing

```sh
wurk done                  # the worktree you are standing in
wurk done some-other-name  # or one you are not
wurk done --yes            # without being asked anything
```

Removes the worktree and deletes the branch. Finishing the one you are standing
in puts you back in the repository afterwards, since where you were is about to
be gone; naming another leaves you where you are.

Work that has landed goes without comment. Anything else is put to you first:

```console
$ wurk done
? PROJ-2222-work-on-login-system is not merged into origin/main and has 2 uncommitted changes. Remove it anyway? [y/N]
```

Anything but `y` leaves it alone. With no terminal to ask on it stops and says
so rather than hanging, so `--yes` is what a script wants. The repository keeps
whatever branch it already had checked out — `done` reports it rather than
changing it. `rm`, `remove` and `delete` all mean `done`.

### Tidying up

For the branches you finished with a while ago, whose pull requests merged
while you were somewhere else:

```console
$ wurk tidy
   BRANCH         CHANGES  VS ORIGIN/MAIN  STATE          PATH
   ─────────────────────────────────────────────────────────────────
   merged-pr      clean    ↓2              merged         merged-pr
   squashed-pr    clean    ↑1 ↓3           squash-merged  squashed-pr

· keeping has-scratch — 1 uncommitted change
· keeping still-open — not merged into origin/main
? remove 2 worktrees? [y/N] y
✓ removed 2 worktrees and 2 branches
```

It fetches first. A branch merged on the forge is not merged *here* until the
remote-tracking branches know about it, and that is exactly the branch worth
tidying away — `--no-fetch` skips that when you are offline. Everything left
standing is listed with the reason, so a survivor is never a mystery.

The worktree you are standing in is always kept: `wurk done` is what takes
that one, since it can move you out first.


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

## Setting up a new worktree

A fresh worktree has none of the things git does not track: no `.env`, no
`node_modules`. Say what to do about it per repository, and it happens every
time one is created.

```toml
[repos."~/dev/myrepo".setup]
copy   = [".env", ".env.local"]   # brought over from the main worktree
link   = ["node_modules"]         # symlinked to it instead, for big directories
run    = ["npm ci"]               # shell commands, in order
script = ".wurk/setup.sh"         # a file to execute last
```

The phases run in that order, so the files a command reads are in place before
it runs. Paths are relative to the repository root, and a source that is not
there is skipped — `.env.local` exists on some machines and not others. A
relative `script` is taken from the new worktree, so a script kept in the
repository runs the branch's own copy of itself.

Commands get the worktree as their working directory and four variables:
`WURK_WORKTREE`, `WURK_REPO`, `WURK_BRANCH` and `WURK_BASE`.

A step that fails stops the ones after it and makes `wurk` exit non-zero.
What becomes of the worktree is yours to choose:

```toml
setup_on_failure = "remove"    # or "keep"
```

| value    | what happens                                                                                                                                                                            |
| -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `remove` | the worktree is taken away again, and the branch with it if wurk created it, and your shell stays where it was — the default, so a worktree that exists is one that is ready to work in |
| `keep`   | the worktree stays, half prepared, and you land in it to fix it                                                                                                                         |

A repository can override the file with `on_failure` in its own `[setup]`.
Either way, `wurk setup` runs the steps again once you have fixed things, and
`wurk <name> --no-setup` skips them in the first place — the pair to reach for
when a `remove` repository keeps taking the evidence away.

## Development

`go test ./...` drives real `git` against throwaway repositories, covering
ordinary merges, squash merges, unmerged work and dirty worktrees.

Built with [cobra](https://github.com/spf13/cobra) and
[lipgloss](https://github.com/charmbracelet/lipgloss).
