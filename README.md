# Generic Jev

[Source repository](https://github.com/aiii-dot-id/aiios-jev-plugin)

`id.aiii.jev` is an AII OS tool plugin. The identity supplies context and focused
questions, receives Jev's answers, and decides what follows. The judge is the one
the operator sets: TypeSafe's hosted Jev, or a judge the operator runs on their own
network that speaks Jev's wire. State and questions are sent to that judge, and only
to it.

**Packaging prerequisite:** use AII Plugin SDK 0.1.18 or newer. Verify that packaged settings preserve `choices_from` before signing.

## Operations

- `query(state, questions)` makes one POST to the configured judge with the configured model.
  `state` is text, an object, or an array. `questions` is Jev's native named map,
  mixing `noul`, `choice`, and `score` questions. This is `write.external` under
  the host's HTTP-verb contract. It requires the normal write-capable grant.
- `models()` lists the models for the plugin card (`read.external`). A malformed
  or oversized list fails as a whole. Duplicate model names appear once.

### Illustrative native mixed request and response

These are complete `query` arguments and the corresponding native service JSON
shape, not a transcript of a live call. All response values (including model ID,
probabilities, confidence, and usage) are **illustrative**, not measurements or
recommended decision thresholds. The plugin adds the configured model to the
outbound request; the caller does not pass it as a query argument.

Request:

```json
{
  "state": {
    "requirement": "The export must preserve non-ASCII names.",
    "evidence": "The export test uses only ASCII names and passes."
  },
  "questions": {
    "covered": {
      "type": "noul",
      "instructions": "Does the evidence show that preservation of non-ASCII names was tested?"
    },
    "conclusion": {
      "type": "choice",
      "instructions": "What does the supplied evidence establish about the requirement?",
      "criteria": {
        "supported": "An observed non-ASCII round trip preserves the names",
        "contradicted": "An observed non-ASCII round trip corrupts or loses the names",
        "insufficient_evidence": "Neither preservation nor corruption of non-ASCII names is demonstrated"
      }
    },
    "evidence_coverage": {
      "type": "score",
      "instructions": "How directly does the supplied test cover the requirement?",
      "criteria": [
        "No export test is supplied",
        "An export test is supplied, but no non-ASCII round trip is tested",
        "A non-ASCII round trip is explicitly tested"
      ]
    }
  }
}
```

Illustrative response (the host's untrusted-result wrapper is not shown):

```json
{
  "model": "jev-1.13.0",
  "answers": {
    "covered": {
      "type": "noul",
      "noul": 0.1
    },
    "conclusion": {
      "type": "choice",
      "choice": "insufficient_evidence",
      "probabilities": {
        "supported": 0.1,
        "contradicted": 0.1,
        "insufficient_evidence": 0.8
      },
      "confidence": 0.6
    },
    "evidence_coverage": {
      "type": "score",
      "score": 1.0,
      "legend": {
        "0": "No export test is supplied",
        "1": "An export test is supplied, but no non-ASCII round trip is tested",
        "2": "A non-ASCII round trip is explicitly tested"
      },
      "probabilities": {
        "0": 0.1,
        "1": 0.8,
        "2": 0.1
      },
      "confidence": 0.6
    }
  },
  "usage": {
    "input_tokens": 100,
    "output_tokens": 20
  }
}
```

Noul returns the probability of yes; it has no separate confidence field.
Choice returns the chosen option, every option's probability, and confidence.
Score uses 2–10 descriptive levels and returns a weighted score, the legend,
probabilities, and confidence. Choice permits 1–255 options. Instructions and
criteria may use structured descriptions as documented by
[TypeSafe](https://docs.typesafe.ai/api). Question/option names are nonblank,
at most 128 bytes. The handler checks the nested contract; the host's supported
schema subset cannot express the entire question map.

Write one judgment per question. Supply the evidence needed to answer it. Include
an insufficient-evidence option when alternatives are not exhaustive. In a
choice about a claim's support, distinguish **insufficient evidence** (the supplied
material establishes neither the claim nor its negation) from **contradiction**
(the supplied material provides evidence against the claim). An ASCII-only test
leaves non-ASCII preservation untested; it does not demonstrate corruption. A
failed non-ASCII round trip would be contradictory evidence. A low noul value or
low service confidence is not a substitute for this explicit choice distinction.
Keep label names consistent with their definitions. Use code for arithmetic and mechanical
checks. Thresholds belong to the caller's evaluated use case. A service confidence
is not a guarantee, and an answer does not authorize a subsequent action.

### Validation diagnostics

Invalid arguments retain `OPERATION_ARGUMENT_INVALID` and identify nested
fields using quoted JSON Pointer paths, for example:

```text
"/questions/coverage/criteria": expected 2 to 10 descriptive levels; got 1
"/questions/route/criteria/retry": expected null, nonblank text, an object, or an array
```

Names containing slashes or tildes are escaped as JSON Pointers; control
characters are quoted rather than emitted as extra lines. Validation is local,
before settings lookup or a provider request.

## Exact behavior and bounds

The plugin forwards only `state`, `questions`, and the configured `model`.
Host metadata is excluded. It loads current settings on each invocation and
cites a credential handle; it never reads the key. There is no file, history,
memory, background, cache, automatic retry, or decision-policy capability.
Existing AII OS tool discovery, untrusted-result handling and interaction history
own the surrounding workflow.

A successful query preserves the service JSON, including additional response
fields, model identity and usage. All requested answer IDs must be present, with
matching types, valid probabilities and the requested options/levels. Missing or
invalid answers fail the call rather than becoming false/zero answers. No
probabilities are normalized, rounded, or thresholded by the plugin.

Requests are at most 256 KiB, the host HTTP body ceiling; this is **not** a token
estimate. The judge still enforces its context limits. Replies are at most the reply
cap (`max_reply_bytes`, 16 KiB unless set), with the call timeout (`timeout_ms`,
10 seconds unless set). An oversized reply fails explicitly, naming the cap; pass
fewer questions or shorter criteria. A smaller input does not guarantee a small reply.

**Delivery requirement:** `prompt.max_tool_result_chars` must be at least the reply
cap plus 3,616 characters: 20,000 at the default cap (the host's default is 32,000).
This accommodates a maximum accepted reply plus its host-owned wrapper. The plugin
cannot inspect this host setting. Qualification checks the default cap through the
conversation loop. A host configured lower can truncate the result with an explicit
notice; that is not complete answer delivery.

Host reason codes are preserved. HTTP 401/403, 429, other non-200 statuses,
invalid replies and oversized replies have distinct failures; when the judge says
when to ask again (`Retry-After`), the failure says it, and a 429 that names no time
says so. A transport failure makes no second attempt and says whether the request
was sent, from the host's receipt:

| Code | Meaning |
|---|---|
| `JEV_TRANSPORT_NOT_SENT` | nothing reached the judge; nothing was answered or charged, and asking again is safe |
| `JEV_TRANSPORT_UNKNOWN` | the request was sent, or is not known to be unsent, and no complete answer came back; another invocation may incur another charge |

**Confirmation stance.** `query` sends the supplied state and questions to the judge
the operator configured and granted, without a separate operator confirmation for
each call: the operator's consent is the judge's settings and grants on the plugin's
card. The case `tests/unconfigured.json` marks it `unconfirmed_by_design`.

### Settings

| Setting | Default | Range | What it sets |
|---|---|---|---|
| `api_key` | — | a credential | Jev's key, for the hosted judge |
| `model` | `jev-latest` | the judge's list, or a versioned ID | the model every request names |
| `backend` | `jev` | `jev`, `local` | where state and questions are sent |
| `local_url` | empty | an http(s) address | the local judge's base address, `http://<host>:<port>`; it answers `/v1/systemone` and `/v1/models`; grant it to the plugin as a local network address (`net.local`). It is sent no key |
| `timeout_ms` | 10000 | 1000–85000 | how long one call waits |
| `max_reply_bytes` | 16384 | 1024–65536 | the largest reply accepted |

## Setup and release boundary

Build with Go 1.25+, TinyGo 0.42+, Python 3, and AII Plugin SDK 0.1.18 or newer. Put `aiisdk` on PATH.

From this repository, run the settings guard immediately after packaging and
before any signing or installation:

```sh
go build ./...
go vet ./...
go test -race ./...
aiisdk build
aiisdk package
python3 scripts/check_package_settings.py dist/pkg/id.aiii.jev-0.1.3
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
JEV_STAGE="$PWD/dist/pkg/id.aiii.jev-0.1.3" ./integration/run.sh
```

The script accepts absolute or caller-relative `AII_OS_DIR` and `JEV_STAGE`
paths (including spaces), resolves them before changing directory, and checks
staged settings before running Go. Its overlay goes under `${TMPDIR:-/tmp}`; a
custom `TMPDIR` must already exist and may also be caller-relative. The temporary
overlay is removed on success, failure, and handled HUP/INT/TERM signals.

The script uses Go's file overlay, leaving the host checkout unchanged. It runs
the actual packaged WASM through a real registry and broker, with ephemeral T2
fixture signatures, a fake credential and an intercepted TypeSafe HTTP endpoint.
It exercises mixed answers, model discovery, malformed/oversized/rate-limited
responses, denied grants, SAFE, cancellation and receipts. A separate
conversation-loop check verifies complete delivery to the next model request.
This proves local integration, not live TypeSafe accuracy or a production release.

## The gate

`scripts/gate.sh` runs every check this repository owns on the candidate's own bytes,
in order: format, vet, unit tests, script tests, build and package, the settings
guard, the worker oracle's cases and confirmation check (`aiisdk test`, with no
network grant), and the integration proof above. It passes only when every step ran
and passed. With `AII_OS_DIR` (a host source checkout) or `AII_OS_BIN` (the release's
worker and verifier) unset, or a tool missing, it ends `INCOMPLETE` (exit 3) before
running anything, naming what is missing; it never skips a step.

```sh
AII_OS_DIR=/path/to/aii-os AII_OS_BIN=/path/to/aii-os-release ./scripts/gate.sh
```

## Ownership and boundaries

Ownership: the identity formulates/interprets; Jev judges; this plugin validates
and transports; AII OS owns authority, credentials, discovery and history.
No ledger/projection/schema change is needed. Falsifier: an accepted query changes
meaning in transit, loses answers without refusal, bypasses host controls, leaks
metadata/credentials, or fails to reach the model intact at the supported limit.
