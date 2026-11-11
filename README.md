# Generic Jev

[Source repository](https://github.com/aiii-dot-id/aiios-jev-plugin)

`id.aiii.jev` is an AII OS tool plugin: the identity's judge. It carries the identity's
questions, about material the identity names but need not read, to the judge the
operator set: TypeSafe's hosted Jev, or a judge the operator runs on their own
network that speaks Jev's wire. It does the bookkeeping code would have done (packing,
ordering, counting) and returns every answer it was paid for, in the fewest words
that keep it whole. Answers are probabilities to weigh, never a decision, a
permission, or who the identity's human is.

**What can leave the machine.** A file the identity names is sent to that judge
without the identity reading it. The host keeps its own places back (the identity's
data directory, key, store and config); it does not look inside files, so a
credential pasted into an ordinary note, a `.env` the identity names, or another
person's mail can be sent. Grant the plugin **files** only if that may happen. There
is no content filtering. Every reply names the judge, and each file it sent with a
digest of the bytes sent.

**Packaging prerequisite:** use an AII Plugin SDK whose `aiiosdk` gives the host's time on each receipt (`LastHostTime`), which the time rule below needs. Verify that packaged settings preserve `choices_from` before signing.

## Operations

- `ask` asks questions about one `state`, or about each of many `items`, in one
  request and one attempt. It is `write.external` (a POST): a read-only grant turns it
  off. It reads the identity's files through `fs.sandbox` when a call names them.
  It is not a primary operation; the identity finds it by search and show.
- `models` lists the judge's models for the plugin card's picker, each labelled with
  its release date and description, and names the judge (`read.external`). A
  malformed or oversized list fails whole; a name listed twice appears once.

### One state

`questions` is a list. Each question has an `id`, Jev's `type` (`noul`, `choice`,
`score` or `box`; a question without one is a noul), `instructions` and `criteria`.
Any value may be a reference, `{"$file": "<path>"}`, read as the identity's own
`read` reads it: text goes in its place, a PNG or JPEG goes to the judge as an image.

```json
{
  "state": {
    "ticket": {"$file": "inbox/0017.eml"},
    "policy": "Refunds within 30 days of purchase."
  },
  "questions": [
    {"id": "covered", "instructions": "Does `ticket` ask for something `policy` covers?"},
    {"id": "route", "type": "choice", "instructions": "Which action does `ticket` ask for?",
     "criteria": {"refund": "money back", "rebook": "a new date", "info": "a question only"}},
    {"id": "urgency", "type": "score", "instructions": "How soon does `ticket` need an answer?",
     "criteria": ["no date is named", "within a week", "today"]}
  ]
}
```

An illustrative reply (the values are not measurements; the host's untrusted-result
wrapper is not shown). Each answer is Jev's, less its `type`, with every probability,
highest first; `sent` names each file put into the request with the first 16 hex
digits of the SHA-256 of its bytes:

```json
{
  "judge": "api.typesafe.ai",
  "model": "jev-1.14.0",
  "answers": {
    "covered": {"noul": 0.92},
    "route": {"choice": "refund", "probabilities": {"refund": 0.88, "rebook": 0.09, "info": 0.03}, "confidence": 0.82},
    "urgency": {"score": 1.7, "probabilities": {"2": 0.8, "1": 0.1, "0": 0.1}, "confidence": 0.55}
  },
  "sent": {"inbox/0017.eml": "0c6e81f2a4b9d750"},
  "usage": {"input_tokens": 412, "output_tokens": 30}
}
```

### Many items

With `items`, each question is asked of each item, the item placed beside the
instructions (`{"question": ..., "item": ...}`; name it `item` in the instructions),
and `state` is shared context, read once. `items` is a list of values (any of which
may be a reference), or `{"$files": "<folder>/<pattern>"}`: one folder, `path.Match`,
no recursion, names in byte order, and a name beginning with `.` only for a pattern
beginning with one. `rank` orders one reply's rows by a question: a noul's yes, a
choice's chosen option or `id.option`, a score's level.

```json
{
  "state": "A ticket asks for a refund when it wants money back for a purchase.",
  "questions": [
    {"id": "refund", "instructions": "Does `item` ask for money back?"},
    {"id": "route", "type": "choice", "instructions": "Which action does `item` ask for?",
     "criteria": {"refund": "money back", "rebook": "a new date", "info": "a question only"}}
  ],
  "items": {"$files": "inbox/*.eml"},
  "rank": "route.refund"
}
```

```json
{
  "judge": "api.typesafe.ai", "model": "jev-1.14.0",
  "rows": [
    {"ref": "inbox/0017.eml", "sha256": "0c6e81f2a4b9d750", "refund": {"noul": 0.98},
     "route": {"choice": "refund", "probabilities": {"refund": 0.91, "rebook": 0.06, "info": 0.03}, "confidence": 0.87}},
    {"ref": "inbox/0004.eml", "sha256": "77f3a2d1e09bc4a8", "refund": {"noul": 0.41},
     "route": {"choice": "rebook", "probabilities": {"rebook": 0.55, "refund": 0.40, "info": 0.05}, "confidence": 0.33}},
    {"ref": "inbox/0009.eml", "refused": "JEV_ITEM_TOO_LARGE: 301214 bytes; one request holds 262144, and the state and questions leave 262031 for an item"}
  ],
  "bands": {"refund": {"yes, above 0.7": 1, "unsettled": 1, "no, below 0.3": 0}},
  "usage": {"input_tokens": 98210, "output_tokens": 410},
  "visited": 120,
  "next": {"after": "inbox/0120.eml", "model": "jev-1.14.0", "screen": "9c1e5a0f3b7d2e6c8a4f1b9d7e3c5a2f6b8d0e4c2a7f9b1d3e5c7a9b2d4f6e8a"}
}
```

One request per call. Items go in whole, in order, while the request stays inside
256 KiB, inside the judge's limits as estimated at 2.4 bytes a token, inside 64
references, and while every row still fits the reply budget; an image item goes
alone. `next` is the way to continue, last in the reply because the host keeps a
long result's tail: pass it back whole as `from`. It is `null` at the screen's end.

- `after` is the last item **consumed**: answered, refused in its row, or a
  nonmatching name passed over. An item that did not fit this batch, and a batch the
  judge did not answer, are not consumed, so nothing is skipped or asked twice.
- `model` is `null` until the screen's first answer binds the version the judge
  reports; every later call sends that version, so an alias that moves changes
  nothing already answered. The judge, the key and the destination stay the
  operator's settings.
- `screen` is the SHA-256 of the resolved state, the resolved questions, and the
  pattern or the items as given. A changed state, question file or items list
  refuses the cursor (`JEV_SCREEN_CHANGED`) before anything is sent. `rank` and
  `long_context` are not part of it, so either may change mid-screen.
- `bands` counts each noul's answers in this reply's rows by TypeSafe's starting
  reading (yes above 0.7, no below 0.3); across calls, add them.
- A file deleted ahead of the cursor is not met; one added behind it is not part of
  the screen; one added ahead is met in turn.
- On this host the folder listing does not page: a screen that needs a name beyond
  the folder's first 1,024 is refused (`JEV_LISTING_INCOMPLETE`). Narrow the folder,
  or name each file as a `$file` item.

### Validation diagnostics

The host holds every call to the schema first and refuses by path; the plugin then
refuses, with `OPERATION_ARGUMENT_INVALID` and a quoted JSON Pointer path, what no
schema can say (an `id` used twice, a `rank` that names no question) and what a
question file holds:

```text
"/questions/2/criteria": expected 2 to 10 descriptive levels; got 1
"/questions/1/criteria/retry": expected null, nonblank text, an object, or an array
```

## Exact behavior and bounds

**References.** A call resolves at most 64 references and at most what one request
holds (256 KiB less the request's own JSON). A file is read by offset in pages and
refused as soon as it passes what is left, so a small reference to a large file costs
one page (`JEV_FILE_TOO_LARGE`). Text is UTF-8 without NUL bytes; an image is a PNG or
a JPEG by its first bytes, sent base64 in `assets` under a reference that is `img-`
and the SHA-256 of its bytes, its path and its place, with the images header. Its
size is read from its header and held to the judge's 280 to 1568 pixels a side before
the rest is read (`JEV_IMAGE_OUT_OF_BOUNDS`); an EXIF orientation other than upright
is said in the reply, since the judge does not apply it. Anything else is
`JEV_FILE_NOT_TEXT`; a file the host could not read is `JEV_FILE_UNREADABLE`; a path
the host refuses keeps the host's own code. Without the operator's **files** grant
(an upgrade from 0.1.x has none until the operator presses it), every reference is
refused with the host's denial and every other call works.

**Replies.** Every answer paid for is in the reply: Jev's answer less two echoes the
identity wrote (each answer's `type`, and a score's `legend`, which is kept when the
criteria came from a file the identity did not read), probabilities highest first,
numbers in their shortest form. A member the judge adds that this plugin does not
know is kept as it came while the reply holds it, and otherwise dropped and named
in `dropped`; so is one under a name the reply uses for its own members (`judge`,
`rows`, `next` and the rest), which never stands beside the plugin's. Each answer's size has a bound from its question alone, so before
anything is sent a question set whose reply could pass the budget is refused, unsent
and uncharged, saying how many questions would fit (`JEV_REPLY_WOULD_NOT_FIT`).
Answers are validated as 0.1.3 validated them (types, ranges, chosen options, levels,
a box's edges, probabilities over exactly the options asked, summing to one within
two-decimal rounding); a missing or invalid answer fails the call, never becoming a
false or zero answer.

**One attempt.** One invocation makes one request, and every outcome comes back at
once, said exactly; nothing is sent twice. The attempt's `timeout_ms` is the setting,
less what the invocation has spent by the host's own clock (its start and the newest
receipt's time), less 5 seconds to build the reply, within the host's 90-second
invocation; under 1 second, nothing is sent.

| Outcome | Code | What the identity is told |
|---|---|---|
| not sent, by the host's receipt | `JEV_TRANSPORT_NOT_SENT` | nothing reached the judge and nothing was charged |
| sent, or not known to be unsent, with no answer | `JEV_TRANSPORT_UNKNOWN` | the answers are unknown and may have been charged |
| refused by status | `JEV_RATE_LIMITED`, `JEV_UNAVAILABLE`, `JEV_ACCESS_REFUSED`, `JEV_REQUEST_REFUSED` | the status, the judge's own words, and `retry_after_s` when it gave one |
| too long (`max_tokens_exceeded`) | `JEV_REQUEST_REFUSED` | the judge's words, and that asking again (with items, the same `from`) with `long_context: true` sends it slower and less accurately |
| too little time left | `JEV_NOT_SENT_OUT_OF_TIME` | nothing was sent |

Among many items, a failed batch consumes nothing of the call: the same `from` asks
again. Too long is only ever the judge's word; the estimate only packs.

What remains, said plainly: a single host call that itself hangs (a read on a
stalled filesystem) cannot be cut short by a guest that cannot wait; if the host
then ends the invocation, the identity sees only the supervisor's words, which do
not say whether a request was sent, and must treat it as unknown and possibly
charged.

**Delivery requirement:** `prompt.max_tool_result_chars` must be at least the reply
budget plus 3,616 characters: 20,000 at the default budget, and the host's default,
32,000, at the largest the setting allows, 28,384. A host configured lower cuts the
result from the front with an explicit notice, losing the first rows; the cursor,
last, survives.

**Confirmation stance.** `ask` sends the state, the questions and the files they name
to the judge the operator configured and granted, without a separate operator
confirmation for each call: the operator's consent is the judge's settings and
grants on the plugin's card. The case `tests/unconfigured.json` marks it
`unconfirmed_by_design`.

### Settings

No setting is removed or reinterpreted from 0.1.x, so an upgrade keeps its
destination, key, model and bounds.

| Setting | Default | Range | What it sets |
|---|---|---|---|
| `api_key` | — | a credential | Jev's key, for the hosted judge; pasting it grants the one host |
| `model` | `jev-latest` | the judge's list, or a versioned ID | the model a screen starts with |
| `backend` | `jev` | `jev`, `local` | where state, questions and the files they name are sent |
| `local_url` | empty | an http(s) address | the local judge's base address, `http://<host>:<port>`; it answers `/v1/systemone` and `/v1/models`; grant it to the plugin as a local network address (`net.local`). It is sent no key |
| `timeout_ms` | 10000 | 1000–60000 | the most one attempt waits, less what the invocation has spent |
| `max_reply_bytes` | 16384 | 1024–28384 | the reply budget; the largest is what the host's default window shows whole |

## Setup and release boundary

Build with Go 1.25+, TinyGo 0.42+, Python 3, and an AII Plugin SDK that carries `LastHostTime`. Put `aiisdk` on PATH.

From this repository, run the settings guard immediately after packaging and
before any signing or installation:

```sh
go build ./...
go vet ./...
go test -race ./...
aiisdk build
aiisdk package
python3 scripts/check_package_settings.py dist/pkg/id.aiii.jev-0.2.0
aiisdk test -skip-build
```

The guard compares the entire `plugin.json` settings array against staged
`install-root/settings.json`, ignoring whitespace and object-key order. Missing,
changed, or added settings metadata fails, including a dropped `choices_from`;
it does not silently repair signed-package inputs. Repackage with the corrected
CLI on failure. This check is about settings preservation, not package integrity,
a signature, or release qualification.

Local script regression tests (no SDK build, network, or host checkout required):

```sh
python3 -B -m unittest discover -s scripts -p 'test_*.py' -v
sh -n integration/run.sh
```

`aiisdk package` preserves the committed `LICENSE` and `THIRD_PARTY_LICENSES` files in the hash-covered install tree.

The SDK emits an **unsigned T0 development package**, which cannot make public
credentialed Jev calls. Release requires the established T2 review or T3 platform
signing ceremony and offline verification under the operator's existing roots.
For the platform ceremony, stage the complete packaged install tree with the
committed helper and the host's signing-input tool:

```sh
go run ./cmd/stage-t3
aii-devsign -staging dist/t3 -payload-out dist/t3/ceremony-payload.json
```

The ceremony signs the `plugin.platform_release` payload. The host tool preserves
the package hash; its manifest omits the SDK's `default_variant`, so the platform
manifest hash is recorded separately. Verify the assembled signed package with
`aii plugin verify` under the release root before publication.

Then install through the host, paste the Jev key on the plugin card, select a
model, and grant its declared host and credential through the existing controls.
The model field also accepts a versioned ID for reproducible evaluations.
No live identity is installed or altered by building or testing this repository.

## Integration proof

After packaging, the integration fixtures require AII OS source with its complete test harness and test dependencies:

```sh
AII_OS_DIR=/path/to/aii-os \
JEV_STAGE="$PWD/dist/pkg/id.aiii.jev-0.2.0" ./integration/run.sh
```

The script accepts absolute or caller-relative `AII_OS_DIR` and `JEV_STAGE`
paths (including spaces), resolves them before changing directory, and checks
staged settings before running Go. Its overlay goes under `${TMPDIR:-/tmp}`; a
custom `TMPDIR` must already exist and may also be caller-relative. The temporary
overlay is removed on success, failure, and handled HUP/INT/TERM signals.

The script uses Go's file overlay, leaving the host checkout unchanged. It runs
the actual packaged WASM through a real registry, broker and worker, with ephemeral T2
fixture signatures, a fake credential and an intercepted TypeSafe HTTP endpoint:
model discovery, a mixed `ask`, malformed and rate-limited replies, too long with its
advice, denied grants, SAFE, cancellation; a reference refused without the files
grant; text and image references from a real sandbox, with the images header and
the digests of the bytes sent; a folder screened to its end through the host's
listing, each item once; a reference into the data directory refused with the
sandbox switch on and off; a shared rule and reference image with two image items;
and the one attempt sized to the invocation's time left, at the edge of the host's
90 seconds (these three take about 90 seconds of wall time). A separate
conversation-loop check verifies complete delivery to the next model request, and
that the cursor survives a window smaller than the reply. This proves local
integration, not live TypeSafe accuracy or a production release.

## The gate

`scripts/gate.sh` runs every check this repository owns on the candidate's own bytes.
It first records the modules the run builds with, as Go resolves them there: the
workspace in effect (`GOWORK`) and each module with its version and the folder it is
read from, so an SDK from outside the repository is never tested unrecorded. Then, in
order: format, vet, unit tests, script tests, build and package, the settings
guard, the worker oracle's cases and confirmation check (`aiisdk test`, with no
network grant), and the integration proof above. It passes only when every step ran
and passed. With `AII_OS_DIR` (a host source checkout) or `AII_OS_BIN` (the release's
worker and verifier) unset, or a tool missing, it ends `INCOMPLETE` (exit 3) before
running anything, naming what is missing; it never skips a step.

```sh
AII_OS_DIR=/path/to/aii-os AII_OS_BIN=/path/to/aii-os-release ./scripts/gate.sh
```

## Ownership and boundaries

Ownership: the identity formulates and interprets; Jev judges; this plugin validates,
resolves the files a call names, packs, and transports; AII OS owns authority,
credentials, the sandbox's walls, discovery and history. No ledger, projection or
schema change is needed, and nothing is stored: the cursor is the identity's.
Falsifier: an accepted `ask` changes meaning in transit, loses or repeats a paid
answer without saying so, skips an item, sends a file the host refused, bypasses host
controls, leaks metadata or credentials, returns a reply past its budget, or outlives
its invocation after sending.
