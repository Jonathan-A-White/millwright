#!/bin/sh
# Hold contrib/vultr-boost, contrib/terraform/vultr-boost and docs/vultr-boost.md
# to what the story promises. Reads this repository and a temporary directory it
# makes and removes. It never reaches the network or spends anything: the wrapper
# runs with a PATH of stand-ins (mw, terraform, ssh, age, age-keygen, wg, curl)
# and a real git, jq and coreutils, over a throwaway vault, home and "tmpfs".
#
# The scenarios are the story's acceptance criteria:
#   a. terraform fmt -check and validate pass, and tflint, and shellcheck over
#      the wrapper, each when installed (and each said to be skipped when not)
#   b. `up --dry-run` and `down --dry-run` print the terraform and ssh steps and
#      run nothing (no stand-in is called, nothing is written)
#   c. no IP, key, token or name of the Governor's is in the committed files, and
#      .gitignore keeps state and variable files out of the repo
#   d. a real run, against stand-ins: tokens reach terraform in its environment
#      and nowhere else (no command line, no file), the state is encrypted into
#      the vault and no plain copy outlives the run, the hub is backed up before
#      its peer changes, a second `up` changes no key, `status` prints the cost,
#      `down` destroys the box and removes the peer
#   e. a missing token or a missing setting is refused in one line, before
#      anything is touched
#   f. docs/vultr-boost.md says up, down, status, the cost and how to revoke,
#      and has the elastic section: the [cloud] table, the cap, idle, snapshot
#   g. the elastic cloud's wrapper (mw-5gr3k0.4): a second box by --name keeps
#      a state and a key of its own and is made from --snapshot; snapshot asks
#      Vultr for a snapshot of the box; spend sums the charges that name the
#      boxes, or says unknown; a bad name is refused; their dry runs call nothing

set -eu

REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null) || REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT=$REPO_ROOT/contrib/vultr-boost
TFDIR=$REPO_ROOT/contrib/terraform/vultr-boost
DOC=$REPO_ROOT/docs/vultr-boost.md

cd "$REPO_ROOT"

fail() {
	echo "check-vultr-boost: $*" >&2
	exit 1
}

# --- the files are there -----------------------------------------------------
[ -f "$SCRIPT" ] || fail "$SCRIPT does not exist"
[ -x "$SCRIPT" ] || fail "$SCRIPT is not executable"
[ -f "$DOC" ] || fail "$DOC does not exist"
for f in versions.tf variables.tf main.tf outputs.tf cloud-init.yaml.tftpl .terraform.lock.hcl; do
	[ -f "$TFDIR/$f" ] || fail "$TFDIR/$f does not exist"
done
bash -n "$SCRIPT" || fail "$SCRIPT does not parse"
sh -n "$0" || fail "$0 does not parse"

# --- a. the linters, each when installed -------------------------------------
if command -v shellcheck >/dev/null 2>&1; then
	shellcheck "$SCRIPT" || fail "shellcheck found something in $SCRIPT"
	shellcheck "$0" || fail "shellcheck found something in $0"
else
	echo "skipped: shellcheck is not installed"
fi

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT INT TERM

if command -v terraform >/dev/null 2>&1; then
	terraform fmt -check -recursive "$TFDIR" || fail "terraform fmt -check: the files are not formatted"
	# validate needs the provider's schema, so init a copy (never the checkout's .terraform)
	mkdir -p "$T/contrib/terraform"
	cp "$REPO_ROOT/contrib/boost-bootstrap.sh" "$T/contrib/"
	cp -R "$TFDIR" "$T/contrib/terraform/vultr-boost"
	rm -rf "$T/contrib/terraform/vultr-boost/.terraform"
	if (cd "$T/contrib/terraform/vultr-boost" && terraform init -backend=false -input=false >"$T/tf-init.log" 2>&1); then
		(cd "$T/contrib/terraform/vultr-boost" && terraform validate) || fail "terraform validate failed"
	else
		echo "skipped: terraform validate (terraform init could not fetch the provider: $(tail -n 1 "$T/tf-init.log"))"
	fi
else
	echo "skipped: terraform is not installed (fmt -check and validate not run)"
fi
if command -v tflint >/dev/null 2>&1; then
	(cd "$TFDIR" && tflint) || fail "tflint found something"
else
	echo "skipped: tflint is not installed"
fi

# --- c. nothing of his in the committed files, and state kept out of git -----
FILES="$SCRIPT $DOC"
for f in "$TFDIR"/*.tf "$TFDIR"/*.tftpl; do FILES="$FILES $f"; done
IPV4='([0-9]{1,3}\.){3}[0-9]{1,3}'
KEY44='[A-Za-z0-9+/]{43}='
TOKENS='ghp_|github_pat_|sk-ant|age1[a-z0-9]{20}|BEGIN [A-Z ]*PRIVATE'
NAMES='jonathan|jawhite|jwhite|allmymind|/home/j|/root/'
bad=0
for f in $FILES; do
	# 0.0.0.0/0 is "anywhere", not an address of his
	if hits=$(grep -n -i -E "$IPV4|$KEY44|$TOKENS|$NAMES" "$f" | grep -v -E '0\.0\.0\.0'); then
		echo "check-vultr-boost: $f holds something that is not generic:" >&2
		echo "$hits" | sed 's/^/    /' >&2
		bad=1
	fi
done
[ "$bad" = 0 ] || exit 1
for pat in '\*\.tfstate' '\*\.tfvars' '\.terraform/'; do
	grep -q "^$pat" "$REPO_ROOT/.gitignore" || fail ".gitignore does not keep $pat out of the repo"
done
if [ -z "$(git ls-files 'contrib/terraform' | grep -E '\.(tfstate|tfvars)' || true)" ]; then :; else
	fail "a state or variable file is tracked under contrib/terraform"
fi

# --- f. the doc ---------------------------------------------------------------
for w in 'vultr-boost up' 'vultr-boost down' 'vultr-boost status' 'cost' 'revoke' 'tfvars' 'wg-enrol' \
	'## Elastic' '[cloud]' 'monthly_cap_usd' 'idle_minutes' 'max_boxes' 'vultr-boost snapshot' 'vultr-boost spend' 'mw cloud check'; do
	grep -q -i -- "$w" "$DOC" || fail "$DOC does not mention: $w"
done

# --- the throwaway machine ----------------------------------------------------
# Nothing from the caller's environment may steer the script.
for v in $(env | sed -n 's/^\(VULTR_BOOST_[A-Z0-9_]*\|MW_[A-Z0-9_]*\|TF_[A-Z0-9_]*\)=.*/\1/p'); do unset "$v"; done

HOME_DIR=$T/home
VAULT=$T/vault
RUN=$T/run
CALLS=$T/calls.log
ENVLOG=$T/tf-env.log
SSHLOG=$T/ssh.log
CURLLOG=$T/curl.log
SECRETS=$T/secrets
BIN=$T/bin
REAL=$T/real
mkdir -p "$HOME_DIR/.config/mw" "$VAULT/hosts/vultr" "$RUN" "$SECRETS" "$BIN" "$REAL"
chmod 700 "$RUN"
: >"$CALLS"
: >"$ENVLOG"
: >"$SSHLOG"
: >"$CURLLOG"
echo "AGE-SECRET-KEY-FAKE" >"$HOME_DIR/.config/mw/age.key"
chmod 600 "$HOME_DIR/.config/mw/age.key"

VULTR_KEY="FAKEvultrAPIkey0123456789"
GH_TOKEN_VALUE="FAKEgithubTOKEN0123456789"
CLAUDE_TOKEN_VALUE="FAKEclaudeTOKEN0123456789"
printf '%s' "$VULTR_KEY" >"$SECRETS/vultr_api_token"
printf '%s' "$GH_TOKEN_VALUE" >"$SECRETS/boost_github_token"
printf '%s' "$CLAUDE_TOKEN_VALUE" >"$SECRETS/boost_claude_token"
WG_PRIV="GGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG="
WG_PUB="PPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP="
HUB_PUB="HHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH="

for tool in sh bash env cat cp mv rm mkdir chmod stat grep sed awk head tail tr cut sort wc date dirname basename \
	mktemp ls printf uname touch sleep true false expr uniq tee diff test base64 find git jq id tty; do
	p=$(command -v "$tool" 2>/dev/null) || continue
	case $p in /*) ln -sf "$p" "$REAL/$tool" ;; esac
done

stub() { # <name>: the body on stdin
	{
		echo '#!/bin/sh'
		cat
	} >"$BIN/$1"
	chmod +x "$BIN/$1"
}

stub mw <<EOF
echo "mw \$*" >>"$CALLS"
[ "\$1" = secrets ] || exit 2
case \$2 in
get) [ -f "$SECRETS/\$3" ] || { echo "no \$3" >&2; exit 1; }; cat "$SECRETS/\$3" ;;
list) ls "$SECRETS" ;;
esac
EOF

stub age-keygen <<EOF
echo "age-keygen \$*" >>"$CALLS"
echo age1fakerecipientfakerecipientfakerecipient
EOF

# "Encrypts" by base64 behind a marker; decrypts it back.
stub age <<EOF
echo "age \$*" >>"$CALLS"
mode=enc
out=
while [ \$# -gt 0 ]; do
	case \$1 in
	-d) mode=dec ;;
	-o) out=\$2; shift ;;
	-r | -i) shift ;;
	*) in=\$1 ;;
	esac
	shift
done
if [ \$mode = enc ]; then
	{ echo AGE-ENCRYPTED; base64; } >"\$out"
else
	tail -n +2 "\${in:?}" | base64 -d
fi
EOF

stub wg <<EOF
echo "wg \$*" >>"$CALLS"
case \$1 in
genkey) echo $WG_PRIV ;;
pubkey) cat >/dev/null; echo $WG_PUB ;;
esac
EOF

stub terraform <<EOF
echo "terraform \$*" >>"$CALLS"
env | grep '^TF_VAR_' >>"$ENVLOG"
state=
sub=
for a in "\$@"; do
	case \$a in
	-chdir=*) ;;
	-state=*) state=\${a#-state=} ;;
	-*) ;;
	*) [ -n "\$sub" ] || sub=\$a ;;
	esac
done
case \$sub in
init) ;;
apply)
	echo "\$state" >"$T/statepath"
	cat >"\$state" <<JSON
{"version":4,"resources":[{"type":"vultr_instance","name":"boost","instances":[{"attributes":{"id":"inst-1234","plan":"vhp-8c-16gb-amd","region":"zzz","label":"cloud1","main_ip":"192.0.2.10","date_created":"2026-10-09T10:00:00+00:00"}}]}]}
JSON
	;;
destroy)
	echo "\$state" >"$T/statepath"
	echo '{"version":4,"resources":[]}' >"\$state"
	;;
esac
EOF

stub ssh <<EOF
echo "ssh \$*" >>"$CALLS"
for last; do :; done
printf '%s\n' "\$last" >>"$SSHLOG"
case \$last in
*"wg-enrol --remove"*) echo "backup:/etc/wireguard/wg0.conf.bak-FAKE"; echo "removed cloud1" ;;
*"wg-enrol "*)
	echo "backup:/etc/wireguard/wg0.conf.bak-FAKE"
	cat <<CONF
# Client config for cloud1 - paste into /etc/wireguard/wg0.conf there.
[Interface]
Address = 10.88.0.9/24
PrivateKey = <PASTE YOUR PRIVATE KEY>

[Peer]
PublicKey = $HUB_PUB
Endpoint = 192.0.2.1:51820
AllowedIPs = 10.88.0.0/24
PersistentKeepalive = 25
CONF
	;;
esac
EOF

stub curl <<EOF
echo "curl \$*" >>"$CALLS"
for last; do :; done
# -K - reads curl's config (the token's header) from stdin
case " \$* " in *" -K - "*) cat >>"$CURLLOG" ;; esac
case \$last in
*/instances/inst-1234) echo '{"instance":{"id":"inst-1234","status":"active","power_status":"running","server_status":"ok","plan":"vhp-8c-16gb-amd","region":"zzz","date_created":"2026-10-09T10:00:00+00:00"}}' ;;
*/plans*) echo '{"plans":[{"id":"vhp-8c-16gb-amd","vcpu_count":8,"ram":16384,"monthly_cost":96,"hourly_cost":0.132}]}' ;;
*/billing/pending-charges) echo '{"pending_charges":[{"description":"cloud1 (vhp-8c-16gb-amd)","total":1.5},{"description":"hub vps","total":6},{"description":"cloud2 (vhp-8c-16gb-amd)","total":0.25},{"description":"cloud12","total":9}]}' ;;
*/snapshots) echo '{"snapshot":{"id":"snap-77","description":"mw boost"}}' ;;
*) echo "curl stand-in: unexpected url \$last" >&2; exit 22 ;;
esac
EOF

cat >"$VAULT/hosts/vultr/vultr-boost.tfvars" <<'EOF'
region      = "zzz"
wg_endpoint = "192.0.2.1:51820"
beads_host  = "192.0.2.2"
EOF
git -C "$VAULT" init -q
git -C "$VAULT" add -A
GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.test GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.test \
	git -C "$VAULT" commit -q -m seed

export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.test GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.test

# run <args...>: the wrapper with the stand-ins first on PATH, a private "tmpfs" and vault.
run() {
	HOME=$HOME_DIR MW_VAULT=$VAULT VULTR_BOOST_RUN=$RUN PATH=$BIN:$REAL "$SCRIPT" "$@"
}
# run_bare <args...>: the same with no stand-ins at all, to prove a dry run calls nothing.
run_bare() {
	HOME=$HOME_DIR MW_VAULT=$VAULT VULTR_BOOST_RUN=$RUN PATH=$REAL "$SCRIPT" "$@"
}

# --- b. a dry run prints the steps and runs nothing --------------------------
for sub in snapshot spend; do
	out=$(run_bare "$sub" --dry-run --name cloud2 2>&1) || fail "$sub --dry-run failed: $out"
	echo "$out" | grep -q 'curl' || fail "$sub --dry-run does not print the call to Vultr: $out"
done
for sub in up down; do
	out=$(run_bare "$sub" --dry-run 2>&1) || fail "$sub --dry-run failed: $out"
	echo "$out" | grep -q 'terraform' || fail "$sub --dry-run does not print the terraform steps: $out"
	echo "$out" | grep -q 'ssh' || fail "$sub --dry-run does not print the ssh steps: $out"
done
[ ! -s "$CALLS" ] || fail "a dry run called a command: $(cat "$CALLS")"
[ -z "$(ls -A "$RUN")" ] || fail "a dry run made files under the tmpfs"
[ -z "$(git -C "$VAULT" status --porcelain)" ] || fail "a dry run changed the vault"
out=$(run_bare up --dry-run 2>&1)
echo "$out" | grep -q 'wg-enrol' || fail "up --dry-run does not name the hub change (wg-enrol): $out"
echo "$out" | grep -q 'apply' || fail "up --dry-run does not name terraform apply: $out"
echo "$out" | grep -q 'destroy' && fail "up --dry-run mentions destroy: $out"
out=$(run_bare down --dry-run 2>&1)
echo "$out" | grep -q 'destroy' || fail "down --dry-run does not name terraform destroy: $out"
echo "$out" | grep -q 'wg-enrol --remove' || fail "down --dry-run does not name the peer removal: $out"

# --- e. refusals, before anything is touched ---------------------------------
mv "$SECRETS/vultr_api_token" "$T/token.away"
if out=$(run up --yes 2>&1); then fail "up without the Vultr token should be refused"; fi
echo "$out" | grep -q 'vultr_api_token' || fail "the refusal does not name the missing token: $out"
[ "$(echo "$out" | wc -l)" -le 3 ] || fail "the refusal is not short: $out"
grep -q '^terraform\|^ssh' "$CALLS" && fail "a refused up still called terraform or ssh"
mv "$T/token.away" "$SECRETS/vultr_api_token"

cp "$VAULT/hosts/vultr/vultr-boost.tfvars" "$T/tfvars.keep"
grep -v '^region' "$T/tfvars.keep" >"$VAULT/hosts/vultr/vultr-boost.tfvars"
if out=$(run up --yes 2>&1); then fail "up without a region should be refused"; fi
echo "$out" | grep -q 'region' || fail "the refusal does not name the missing setting: $out"
grep -q '^terraform\|^ssh' "$CALLS" && fail "a refused up still called terraform or ssh"
cp "$T/tfvars.keep" "$VAULT/hosts/vultr/vultr-boost.tfvars"

if out=$(run sideways 2>&1); then fail "an unknown command should be refused"; fi
grep -q '^terraform\|^ssh' "$CALLS" && fail "an unknown command called terraform or ssh"

# --- d. a real run -----------------------------------------------------------
# status with nothing up: no box, the price a box would cost, no token needed
out=$(run status 2>&1) || fail "status with no box failed: $out"
echo "$out" | grep -q -i 'no box' || fail "status with no box does not say so: $out"
echo "$out" | grep -q '96' || fail "status with no box does not give the monthly cost: $out"

# up, asking terraform to confirm (no --yes)
out=$(run up 2>&1) || fail "up failed: $out"
grep -q '^terraform .*init' "$CALLS" || fail "up did not terraform init"
apply=$(grep '^terraform .* apply' "$CALLS") || fail "up did not terraform apply"
echo "$apply" | grep -q -- '-auto-approve' && fail "up without --yes passed -auto-approve"
echo "$apply" | grep -q -- "-var-file=$VAULT/hosts/vultr/vultr-boost.tfvars" || fail "apply does not read the vault's tfvars: $apply"
state_path=$(cat "$T/statepath")
case $state_path in "$RUN"/*) ;; *) fail "terraform's state was kept at $state_path, not under the private tmpfs $RUN" ;; esac
[ ! -e "$state_path" ] || fail "the plain state outlived the run: $state_path"
[ -z "$(ls -A "$RUN")" ] || fail "the tmpfs directory outlived the run: $(ls -A "$RUN")"

# the hub is backed up first, then changed, and the way back is printed
grep -q 'cp -p' "$SSHLOG" || fail "the hub's wg0.conf was not backed up before the change"
grep -q "wg-enrol cloud1 $WG_PUB" "$SSHLOG" || fail "the peer was not enrolled with the box's public key: $(cat "$SSHLOG")"
echo "$out" | grep -q 'wg0.conf.bak-FAKE' || fail "the backup's path is not printed: $out"
echo "$out" | grep -q 'wg-enrol --remove cloud1' || fail "the way back is not printed: $out"
n_cp=$(awk '/cp -p/ { print NR; exit }' "$SSHLOG")
n_enrol=$(awk '/wg-enrol cloud1/ { print NR; exit }' "$SSHLOG")
[ "$n_cp" -le "$n_enrol" ] || fail "the hub was changed before it was backed up"

# the tokens reach terraform in its environment, the address and the hub's key come from the hub
grep -q "TF_VAR_vultr_api_key=$VULTR_KEY" "$ENVLOG" || fail "terraform did not get the Vultr token in its environment"
grep -q "TF_VAR_github_token=$GH_TOKEN_VALUE" "$ENVLOG" || fail "terraform did not get the GitHub token in its environment"
grep -q "TF_VAR_claude_token=$CLAUDE_TOKEN_VALUE" "$ENVLOG" || fail "terraform did not get the Claude token in its environment"
grep -q "TF_VAR_wg_private_key=$WG_PRIV" "$ENVLOG" || fail "terraform did not get the box's WireGuard key"
grep -q 'TF_VAR_wg_address=10.88.0.9' "$ENVLOG" || fail "terraform was not given the address the hub assigned"
grep -q "TF_VAR_wg_hub_pubkey=$HUB_PUB" "$ENVLOG" || fail "terraform was not given the hub's public key"

# ...and they are nowhere else: not a command line, not a curl config, not output, not a file
for secret in "$VULTR_KEY" "$GH_TOKEN_VALUE" "$CLAUDE_TOKEN_VALUE" "$WG_PRIV"; do
	grep -q "$secret" "$CALLS" && fail "a secret was on a command line: $(grep "$secret" "$CALLS")"
	echo "$out" | grep -q "$secret" && fail "a secret was printed"
	if grep -rl "$secret" "$VAULT" "$REPO_ROOT/contrib" "$RUN" >/dev/null 2>&1; then
		fail "a secret was written in the clear: $(grep -rl "$secret" "$VAULT" "$REPO_ROOT/contrib" "$RUN")"
	fi
done

# the state and the key are in the vault, encrypted; the public key is plain; all committed
for f in terraform.tfstate.age wg.key.age; do
	[ -f "$VAULT/hosts/vultr/$f" ] || fail "hosts/vultr/$f was not kept in the vault"
	[ "$(head -n 1 "$VAULT/hosts/vultr/$f")" = AGE-ENCRYPTED ] || fail "hosts/vultr/$f is not encrypted"
done
grep -q 'inst-1234' "$VAULT/hosts/vultr/terraform.tfstate.age" && fail "the state in the vault is readable"
[ "$(cat "$VAULT/hosts/vultr/wg.pub")" = "$WG_PUB" ] || fail "hosts/vultr/wg.pub is not the public key"
[ -z "$(git -C "$VAULT" status --porcelain)" ] || fail "the vault is left with uncommitted files: $(git -C "$VAULT" status --porcelain)"
git -C "$VAULT" log --format=%s | grep -q '^vultr-boost' || fail "the state was not committed in the vault"
[ -z "$(find "$VAULT" "$REPO_ROOT" -path "$REPO_ROOT/.git" -prune -o -path "$VAULT/.git" -prune -o \( -name '*.tfstate' -o -name '*.tfstate.backup' -o -name '*.tfplan' \) -print)" ] ||
	fail "a plain state file is lying in the vault or the repo"

# a second up: the same key, the peer enrolled again, nothing broken
out=$(run up --yes 2>&1) || fail "the second up failed: $out"
[ "$(grep -c '^wg genkey' "$CALLS")" = 1 ] || fail "the second up made a new WireGuard key"
grep '^terraform .* apply' "$CALLS" | tail -n 1 | grep -q -- '-auto-approve' || fail "up --yes did not pass -auto-approve"
grep -q 'inst-1234' "$VAULT/hosts/vultr/terraform.tfstate.age" && fail "the state in the vault is readable after the second up"

# status with the box up: its state and its cost
out=$(run status 2>&1) || fail "status failed: $out"
echo "$out" | grep -q 'active' || fail "status does not give the box's state: $out"
echo "$out" | grep -q 'inst-1234' || fail "status does not name the box: $out"
echo "$out" | grep -q '0.132' || fail "status does not give the hourly cost: $out"
echo "$out" | grep -q '96' || fail "status does not give the monthly cost: $out"
grep -q "$VULTR_KEY" "$CURLLOG" || fail "status did not send the Vultr token to curl (as a config on stdin)"
grep -q "$VULTR_KEY" "$CALLS" && fail "status put the Vultr token on a command line"
[ -z "$(ls -A "$RUN")" ] || fail "status left a tmpfs directory behind"

# down: destroy, then take the peer off the hub
: >"$SSHLOG"
out=$(run down --yes 2>&1) || fail "down failed: $out"
grep '^terraform .* destroy' "$CALLS" | grep -q -- '-auto-approve' || fail "down --yes did not terraform destroy -auto-approve"
grep -q 'wg-enrol --remove cloud1' "$SSHLOG" || fail "down did not remove the peer from the hub: $(cat "$SSHLOG")"
grep -q 'cp -p' "$SSHLOG" || fail "down did not back up the hub's wg0.conf first"
[ -z "$(ls -A "$RUN")" ] || fail "down left a tmpfs directory behind"
out=$(run status 2>&1) || fail "status after down failed: $out"
echo "$out" | grep -q -i 'no box' || fail "status after down does not say there is no box: $out"
[ -z "$(git -C "$VAULT" status --porcelain)" ] || fail "the vault is left with uncommitted files after down"

# --- g. the elastic cloud's wrapper --------------------------------------------
if out=$(run up --yes --name Cloud_2 2>&1); then fail "a box name wg-enrol would not take should be refused"; fi
echo "$out" | grep -q 'a-z0-9' || fail "the refusal of a bad name does not say what a name is: $out"

cp "$VAULT/hosts/vultr/terraform.tfstate.age" "$T/cloud1.state.keep"
: >"$SSHLOG"
out=$(run up --yes --name cloud2 --snapshot snap-9 2>&1) || fail "up --name cloud2 failed: $out"
apply=$(grep '^terraform .* apply' "$CALLS" | tail -n 1)
echo "$apply" | grep -q -- '-var name=cloud2' || fail "up --name cloud2 did not make the box under its name: $apply"
echo "$apply" | grep -q -- '-var snapshot_id=snap-9' || fail "up --snapshot did not make the box from the snapshot: $apply"
grep -q "wg-enrol cloud2 $WG_PUB" "$SSHLOG" || fail "cloud2 was not enrolled on the hub under its name: $(cat "$SSHLOG")"
for f in cloud2.tfstate.age cloud2.wg.key.age cloud2.wg.pub; do
	[ -f "$VAULT/hosts/vultr/$f" ] || fail "cloud2 has no hosts/vultr/$f of its own"
done
cmp -s "$T/cloud1.state.keep" "$VAULT/hosts/vultr/terraform.tfstate.age" || fail "making cloud2 changed cloud1's state"
[ -z "$(git -C "$VAULT" status --porcelain)" ] || fail "up --name cloud2 left the vault uncommitted"

: >"$CURLLOG"
out=$(run snapshot --name cloud2 2>&1) || fail "snapshot failed: $out"
grep -q '^curl .*-X POST.*instance_id.*inst-1234.*/snapshots' "$CALLS" || fail "snapshot did not ask Vultr to snapshot cloud2's box: $(grep '^curl' "$CALLS" | tail -n 1)"
echo "$out" | grep -q 'snap-77' || fail "snapshot does not print the snapshot's id: $out"
echo "$out" | grep -q 'snapshot = "snap-77"' || fail "snapshot does not say where the id goes: $out"
grep -q "$VULTR_KEY" "$CURLLOG" || fail "snapshot did not send the token to curl on stdin"
grep -q "$VULTR_KEY" "$CALLS" && fail "snapshot put the Vultr token on a command line"
[ -z "$(ls -A "$RUN")" ] || fail "snapshot left a tmpfs directory behind"

: >"$SSHLOG"
out=$(run down --yes --name cloud2 2>&1) || fail "down --name cloud2 failed: $out"
grep '^terraform .* destroy' "$CALLS" | tail -n 1 | grep -q -- '-var name=cloud2' || fail "down --name cloud2 did not destroy cloud2"
grep -q 'wg-enrol --remove cloud2' "$SSHLOG" || fail "down --name cloud2 did not remove its peer: $(cat "$SSHLOG")"

out=$(run spend --name cloud1 --name cloud2 2>&1) || fail "spend failed: $out"
[ "$out" = 1.75 ] || fail "spend should sum cloud1's and cloud2's charges and nothing else (1.75), not: $out"
mv "$SECRETS/vultr_api_token" "$T/token.away"
out=$(run spend --name cloud1 2>&1) || fail "spend without a token failed: $out"
[ "$out" = unknown ] || fail "spend that cannot ask Vultr should say unknown, not: $out"
mv "$T/token.away" "$SECRETS/vultr_api_token"

echo "check-vultr-boost: ok"
