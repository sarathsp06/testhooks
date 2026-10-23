# Testhooks: made for humans, and for machines

This file is for anyone changing Testhooks, person or agent. It says what the
product is for and the rules every change has to keep. Build and test commands,
layout and architecture are in [CONTRIBUTING.md](CONTRIBUTING.md).

## The tagline, and what it commits us to

**Made for humans, and for machines.** A person gets a webhook URL in one click
and understands every screen without reading docs. A script or an agent gets the
same thing through the REST API and WebSocket, with the same words and the same
defaults. Nothing is UI-only, and nothing is API-only.

**Hard to get wrong.** Testhooks sits between a provider and someone's code, so a
mistake here looks like a bug somewhere else. The product follows the principle
of least astonishment: the obvious action is the safe one, and the result of any
action is the one its label promised.

## Rules for every change

1. **Plain words, the user's words.** Labels say what happens, not how it is
   implemented. "Reply to the sender with my app's response", not "sync
   forward mode". No jargon a first-time user has to look up; where a technical
   term is unavoidable (CORS, HMAC), say what to do about it in the same sentence.
2. **Safe defaults.** A new endpoint captures and does nothing else. Forwarding
   is off until a URL is set. Scripts do not run until saved. Signature
   enforcement is off until asked for. Secrets are write-only once stored.
3. **Nothing changes until it is saved, and you can see it before you save.**
   The script editor tries a transform or reply on real captured traffic and
   shows a diff and "what the sender gets" before Save. Any new feature that
   alters what reaches a sender or a target gets the same preview.
4. **Destructive actions confirm; everything else does not.** Deleting an
   endpoint or a script asks once. Saving settings, replaying a request or
   switching modes never pops a dialog.
5. **Failures are visible and named.** A forward that failed, a script that
   threw, a signature that did not verify: each shows on the request with the
   reason, and the sender gets a correct status (502 for a dead target, 401 for
   a bad signature when enforcing), never a silent 200.
6. **The same behaviour in both modes.** Server mode and browser mode run the
   same pipeline (capture, transform, forward, reply) with the same config
   fields. A setting that only works in one mode says so where it is set.
7. **Every UI action is an API call.** If a screen can do it, `curl` can do it
   with the fields documented in the REST API reference. New config lives in
   the endpoint `config` object and is documented the day it ships.
8. **One binary, zero required dependencies to try it.** `testhooks` runs in
   memory with the UI embedded. PostgreSQL is for keeping data, not for starting.
9. **Docs are part of the change.** A behaviour change ships with its guide and
   reference updated in the same commit. The docs use the same words as the UI.
10. **No surprises for the sender.** Captured requests are forwarded byte for
    byte unless a transform says otherwise. Hop-by-hop and security-sensitive
    headers are stripped; everything else passes through untouched.

## Before you open a pull request

- Can a first-time user predict what each new label does? If not, rename it.
- Does the API expose it, and does `docs/src/content/docs/reference/api.md` say so?
- Does the user see the result before it takes effect, when it affects a sender or target?
- Is the failure path visible, with a status code a real receiver would send?
- Do `go test ./...`, `npm run check` and both builds pass?
