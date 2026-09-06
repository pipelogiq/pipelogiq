# Pipelogiq commercial licence

## What this covers

This document covers two things, and nothing else:

1. The contents of `apps/go/internal/pro/` that are compiled only under the
   `pro` build tag — the files whose first line is `//go:build pro`.
2. The licence token supplied in `PIPELOGIQ_LICENSE` and the private key that
   signs it.

**Everything else in this repository is Apache-2.0 and stays that way.** That
includes every file not listed above: the control plane, the worker, the
scheduler, the SDK contracts, the dashboard, the documentation, and the licence
*verification* code in `apps/go/internal/license/`. Adding a commercial tier next
to the core does not relicense any existing code, and never will.

The build seam is deliberate and inspectable. `apps/go/internal/pro/pro.go` and
`apps/go/internal/pro/stub.go` are Apache-2.0: they declare the interfaces and
the "not licensed" implementations. A clone of this repository builds, tests, and
runs with no commercial code involved.

## What you may do with a valid licence

Subject to a current licence for your organization, you may run the `pro`-tagged
build in production, for as many deployments as you operate, within the engineer
band named in the token.

You may read the `pro`-tagged source, modify it for your own deployment, and
audit it. You may not redistribute it, publish it, or make it available to
anyone outside your organization, in source or compiled form.

You may not share, resell, or publish a licence token. A token is issued to one
organization.

## What a licence does not do

- **It cannot stop your pipelines.** No code path fails, pauses, or degrades a
  pipeline because of a licence. Gating happens at the API surface, before work
  is created. An expired licence means new Pro *requests* answer `402`; work
  already in the system runs to completion.
- **It does not phone home.** Verification is a local Ed25519 signature check
  against a public key compiled into the binary. There is no telemetry, no
  activation call, and no network path in the licensing code — by construction,
  not by configuration.
- **It does not meter you.** There is no usage counting and no per-seat
  enforcement. The engineer band is a commercial term, stated in the token and
  shown in the dashboard; the software does not police it.
- **It does not expire abruptly.** An expired licence keeps every feature for a
  14-day grace period, with a banner in the dashboard. After that the deployment
  is Community — still complete for a single developer.

## Tiers

| Tier | Band | Price |
|---|---|---|
| Community | — | Free, Apache-2.0, everything a developer needs |
| Pro Team | ≤ 50 engineers | €2,400 / year |
| Pro Business | ≤ 250 engineers | €6,000 / year |
| Pro Scale | 250+ engineers | €12,000 / year |
| Enterprise | — | from €25,000 / year, with an SLA, source escrow, and an air-gap bundle |

Services offered alongside: a fixed-price "Migrate from Hangfire in a week"
engagement, a design-partner program (five seats), and an Agent Governance
workshop.

Pricing is restated on the pricing page; the two must agree. If they disagree,
the pricing page is authoritative for a new purchase and this file is
authoritative for what the code does.

## Warranty and liability

The `pro`-tagged code is provided without warranty of any kind, express or
implied. In no event shall liability exceed the fees paid for the licence term in
which a claim arose.

## Termination

A licence ends when its token expires and is not renewed. On termination, stop
running the `pro`-tagged build. Nothing needs to be uninstalled and no data needs
to be deleted: the Community build reads the same database, and a Community
deployment runs every pipeline the Pro one did.

## Contact

licensing@pipelogiq.dev
