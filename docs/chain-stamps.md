# Chain stamps

A chain stamp is a record that says a rig's branch held a commit, without saying
which rig or which commit. This page is a stub: the record kind is built and
verified in code (`domain/stamp.go`, `application/stamp.go`); sending one, and
the check that reads them back, come in the epic's later stories.

## The record

The payload is a postern record (docs/protocol.md section 1 of
github.com/Jonathan-A-White/postern) of kind `stamp`, framed as `nftgate`
version 1 like a message. Fields, in this order:

| Field | In the clear | What it holds |
| --- | --- | --- |
| `v` | yes | Always 1. |
| `kind` | yes | Always `stamp`. |
| `to` | yes | The Governor's compressed public key, hex. |
| `from` | yes | The sender's compressed public key, hex. |
| `ts` | yes | Unix seconds, when the sender built it. |
| `commitment` | yes | Lower-case hex SHA-256 of the rig, a newline, then the full commit id. |
| `ct` | no | The body, sealed to `to` by the same Cipher as a message's text. |

The body is JSON: `rig`, `branch`, `commit`, `story`, `title` (the commit's
subject), `host`, `at` (UTC, RFC 3339). Nothing of it but the commitment is in the
clear, so the chain shows neither the rig nor the commit.

## Verifying

Whoever holds a rig name and a commit id recomputes the commitment and compares
(`domain.Stamp.Verify`). `application.OpenStamp` also refuses a sealed body whose
rig and commit do not make the commitment shown beside it.
