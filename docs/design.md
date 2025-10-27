# PortNoteProMax Architecture Overview

## Goals
- Track custom servers and their ports with notes and fingerprint-based defaults.
- Provide real-time port status updates with manual refresh controls and naabu-powered sweeps.
- Allow hiding/showing ports (including bulk operations) with immediate UI feedback.
- Support host management with on-demand full-range port scanning.
- Secure the system with user authentication.
- Deliver a responsive, card-based UI inspired by Docker Compose Maker.
- Package the application for Docker/Docker Compose deployment, suitable for Docker Hub.

## High-Level Architecture

### Backend (Go)
- **Framework**: Standard `net/http` with `chi` router for lightweight middleware support.
- **Authentication**: Session-based login backed by bcrypt-hashed credentials (stored in SQLite).
- **Persistence**: SQLite database via `modernc.org/sqlite` driver (pure Go, plays nicely in Docker).
- **Port Tracking**:
- Manual refresh endpoint triggers an immediate full-range (1-65535) scan for a host via naabu, auto-creating records for detected open ports while preserving existing fingerprints.
  - Port status detection uses TCP dial with timeout.
- **API Surface**:
  - Auth routes: login, logout.
  - Host management: list/create/update/delete, trigger scan.
  - Port management: add/remove/update note/toggle hidden/bulk hide/unhide.
  - Real-time updates: Server-Sent Events (SSE) stream for immediate UI refresh on changes.
- **Templates/Assets**: Go `html/template` for SSR shell; JS handles SSE, manual refresh controls, and bulk operations.

### Frontend
- Responsive layout using Tailwind-inspired utility classes (hand-authored CSS).
- Dashboard view:
  - Host selector sidebar listing all servers with quick usage counts.
  - Main area displays only active (open) ports as card tiles focused on service fingerprint.
  - Hidden ports tab shows dashed-outline cards to distinguish concealed entries.
  - Controls for manual refresh, add/delete ports, bulk hide/unhide/delete, unused-port suggestions.
  - Search, sorting, and pagination UI for port grids.
- Modal forms (vanilla JS) for host/port CRUD.
- SSE listener updates port status and hidden state in real-time.
- Fallback polling when SSE disconnects.

### Background Tasks
- `scanner.Manager` maintains job queue.
- Uses worker goroutines triggered on:
  - Manual refresh actions (full naabu scan).
  - Targeted scans on add/update/bulk operations.

### Configuration
- `config.yaml` plus environment overrides:
  - Admin credentials (username/password; password stored hashed on first run or provided as hash).
  - Scan concurrency and timeout settings.
- Command-line flags for alternative config paths.

### Dockerization
- Multi-stage Dockerfile: build Go binary, copy static assets.
- Docker Compose:
  - `app` service running Go binary.
  - Named volume for SQLite persistence.
  - Environment variables for admin credentials and scan parameters.
- Provide helper script for building and pushing Docker image to Docker Hub.

## Data Model
- `users` (id, username, password_hash, created_at).
- `hosts` (id, name, address, auto_scan, created_at, updated_at).
- `ports` (id, host_id, number, note, fingerprint, hidden, status, last_checked).
- `port_events` (id, port_id, status, checked_at) for history (optional).

## Security Considerations
- Enforce HTTPS via reverse proxy recommendation (documented).
- CSRF protection on form posts using tokens tied to session.
- Rate limiting on login attempts.
- Password hashing via bcrypt with configurable cost.

## External Interfaces
- No third-party network access besides host scanning.
- Docker deployment documented with env variable tables and sample commands.

## Testing Strategy
- Unit tests for auth, scanner, and handlers.
- Integration test spinning up in-memory SQLite.
- E2E smoke using `go test` with httptest server.

## Deliverables
- Go source (`cmd/server`, `internal/...`).
- Static assets (`web/static`, `web/templates`).
- Configuration samples.
- Dockerfile + docker-compose.yml.
- README with setup, usage, and Docker Hub publish instructions.
