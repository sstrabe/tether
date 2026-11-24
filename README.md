# Tether

An iOS client for a Tailscale tailnet, built to stay connected on mobile networks. It runs the
open-source Tailscale engine (`tailscale.com`, Go) in a packet tunnel extension. Unofficial: see
`NOTICE`.

## Why

The official app drops its relay (DERP) connection on mobile networks in bursts, dozens of times a
day (seen 2026-11-23: 78 reconnects from one phone in a day, up to ten in five minutes, many hung up
right after the server's greeting). Reading the engine shows a loop:

1. iOS reports a network path update (on cellular, often just a new temporary IPv6 address).
2. The engine counts any changed routable address as a *major* change: it rebinds its sockets,
   pings the DERP server with a 3 s timeout (and drops the connection if the radio is slow to
   wake), and re-applies the VPN's network settings.
3. Re-applying network settings makes iOS report another path update. Back to 1.

## What Tether does differently

| Where | Change |
|---|---|
| `core/tether/path.go` | Path updates are coalesced: only a different interface or address family, after 2 s of quiet, reaches the engine. Repeats and bounce-backs are dropped. |
| `core/tether/settings.go` | Network settings go to iOS only when they differ from what was last applied, which breaks the loop above. |
| `core/patches/0001-mobile-rebind.patch` | `netmon.IgnoreAddedIPs`: an interface that only *gained* addresses is a minor change. `magicsock.DERPRebindPingTimeout/Tries`: 8 s, two tries before a DERP connection is given up after a rebind. Both off by default; the core turns them on. |
| App | Always-on (an on-demand rule that reconnects whenever the tunnel is down), `disconnectOnSleep = false`. |
| Core | No log upload to Tailscale; Go memory limit 30 MB (the extension's cap is 50 MB); features a phone doesn't use left out at build time (`core/build-tags.txt`). |
| Diagnostics | Counters for path updates seen vs. passed on and settings applied vs. skipped, memory, and a log that survives extension restarts, with export. |

The control server is a setting (or `ControlURL` in an MDM managed app configuration), never in the
source.

## Layout

- `core/`: the Go module. `tether/` is the engine (start, stop, login, status, path changes);
  `cmd/libtether` the C API the app and extension link (`-buildmode=c-archive`); `cmd/harness` runs
  the engine in userspace on Linux against a real control server. `scripts/tailscale-src.sh` puts
  the patched Tailscale source in `third_party/` (gitignored; `go.mod` replaces `tailscale.com`
  with it). `scripts/build-ios.sh` builds `libtether.a` for the SDK being built (Xcode script phase).
- `Tunnel/`: `PacketTunnelProvider`: finds the utun descriptor, starts the core with it, applies
  the network settings it asks for, feeds it NWPathMonitor updates and wake-ups, answers the app.
- `App/`: SwiftUI app: on/off, sign-in, devices, settings (control server, exit node, routes, DNS,
  always-on), diagnostics, log. Engine "In the app (test)" runs the core in the app without a
  tunnel: the default in the simulator, where packet tunnels can't run.
- `Shared/`: the Swift wrapper of the C API (`Core.swift`), models, the bridging header.
- `project.yml`: XcodeGen. Bundle ids `dev.sstrabe.tether` and `dev.sstrabe.tether.tunnel`, app
  group `group.dev.sstrabe.tether`.

## Build and test

Go is not needed on the laptop: `./dgo '<cmd>'` runs Go in a container in `core/`.

```sh
./dgo './scripts/tailscale-src.sh && go test -tags "$(cat build-tags.txt)" ./...'
./dgo 'go build -tags "$(cat build-tags.txt)" -o /src/harness-bin ./cmd/harness'
./harness-bin -control https://<control server> -state ./scratch-state -login=false
```

CI (`.github/workflows/build.yml`) runs the Go tests, the simulator build and tests, and an
unsigned device build, and keeps `Tether-sim.zip` (the simulator app) as an artifact.

**On a device** this needs the Network Extension entitlement, which only the paid Apple Developer
Program grants: a free Personal Team can build it but not sign it. The Mac also needs Go for the
script phase (`brew install go`).

## State

- Core: builds and runs (Linux harness reaches the control server and waits for login; 8 MB heap,
  24 MB from the OS). Unit tests for the path filter, settings dedupe and the netmon patch.
- iOS: written, compiled only by CI so far. Not yet run on a device (entitlement).
- Not done: Taildrop, SSH, serve, MagicDNS hosts file beyond what DNS gives, exit-node picker
  by name only, tailnet lock signing from the app (sign new nodes from another device).
