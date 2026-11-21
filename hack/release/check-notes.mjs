// Renders release notes with .releaserc.json's generator configuration from
// the repository's own commits, as if the remote were a private forge, and
// fails if any link leads anywhere but the public GitHub repository — or a
// GitHub profile, which is what the preset makes of an @mention, as GitHub
// itself would.
// semantic-release would otherwise take the links from the remote it runs
// against — the internal Forgejo — and write them into CHANGELOG.md on the
// public main. CI runs it with the same pinned packages as main.yml.
//
//   node hack/release/check-notes.mjs [repo]
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { generateNotes } from "@semantic-release/release-notes-generator";

const repo = process.argv[2] ?? ".";
const PUBLIC = "https://github.com/metapatrol/ferry/";

const rc = JSON.parse(readFileSync(`${repo}/.releaserc.json`, "utf8"));
const GENERATOR = "@semantic-release/release-notes-generator";
const entry = rc.plugins.find((p) => p === GENERATOR || (Array.isArray(p) && p[0] === GENERATOR));
if (!entry) throw new Error(".releaserc.json configures no release-notes-generator");
const config = Array.isArray(entry) ? entry[1] ?? {} : {};

const SEP = "==ferry-commit-end==";
const log = execFileSync("git", ["-C", repo, "log", "-200", `--format=%H%n%B${SEP}`], { maxBuffer: 1 << 26 }).toString();
const commits = log.split(`${SEP}\n`).filter((c) => c.trim()).map((c) => {
  const [hash, ...message] = c.split("\n");
  return { hash, message: message.join("\n") };
});
// Commits that carry what the history may not: a breaking change, a
// reference and a mention.
commits.push(
  { hash: "0123456789abcdef0123456789abcdef01234567", message: "fix: a fix (#12)\n\nCloses #3" },
  { hash: "fedcba9876543210fedcba9876543210fedcba98", message: "feat!: a break by @alex\n\nBREAKING CHANGE: changed" },
);

const notes = await generateNotes(config, {
  cwd: repo,
  commits,
  lastRelease: { gitTag: "v0.1.0", gitHead: "a" },
  nextRelease: { gitTag: "v0.2.0", gitHead: "b", version: "0.2.0" },
  options: { repositoryUrl: "https://forge.internal.invalid/mdvc/ferry.git" },
  logger: console,
});

const links = notes.match(/https?:\/\/[^\s)>\]]+/g) ?? [];
const profile = /^https:\/\/github\.com\/[a-z0-9-]+$/i;
const bad = links.filter((l) => !l.startsWith(PUBLIC) && !profile.test(l));
if (links.length === 0) {
  console.error("ERROR: the release notes carry no links at all; the check proves nothing");
  process.exit(1);
}
if (bad.length > 0 || notes.includes("internal.invalid")) {
  console.error("ERROR: release notes link outside " + PUBLIC + ":\n  " + [...new Set(bad)].join("\n  "));
  process.exit(1);
}
console.log(`release notes: ${links.length} links, all to ${PUBLIC}`);
