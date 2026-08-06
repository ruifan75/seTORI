# seTORI Copilot Instructions

**Project**: A VTuber song database and streaming performance tracker with AI-powered analysis.

## Architecture Overview

### 🏗️ System Components

**Backend (Go)**: REST API with PostgreSQL, featuring:
- Service layer pattern: business logic isolated from HTTP handlers ([handler/router.go](handler/router.go#L36))
- Repository pattern for database access via [internal/repository](internal/repository)
- Domain models in [internal/models](internal/models) (Singer, Song, Stream, Performance)
- Core services: SongService, StreamService, SingerService, HolodexService, CommentService, NormalizationService, PerformanceService, EndTimeEstimateService

**Frontend (React + TypeScript + Vite)**: 
- Uses [React Query](frontend/package.json) for server state management
- Zustand for client state
- Tailwind CSS for styling
- API client in [frontend/src/api/client.ts](frontend/src/api/client.ts#L1) with Axios and error handling
- Type-safe API responses via [frontend/src/api/types.ts](frontend/src/api/types.ts#L56)

**Data Layer**: PostgreSQL with embedded SQL migrations ([backend/internal/database/migrations](backend/internal/database/migrations))

**External Integrations**:
- **Holodex API**: Song setlists from VTuber streams
- **YouTube API**: Comment extraction and video metadata
- **iTunes API**: Song information lookup via [pkg/itunes](backend/pkg/itunes)
- **Groq API**: AI-powered song name normalization

### 📊 Data Flow

1. **Stream Ingestion**: Fetch stream metadata via Holodex/YouTube → Store in `streams` table
2. **Song Detection**: Analyze comments or Holodex setlist → Extract songs → Link to master `songs` table
3. **Performance Tracking**: Record song plays as `performances` (stream_id + song_id + timestamp range)
4. **Normalization**: Use Groq AI to deduplicate/correct song names across sources
5. **Enrichment**: Query iTunes for track metadata, estimate end times via audio analysis

## Key Patterns & Conventions

### Backend Patterns

**Service Architecture** ([internal/service](backend/internal/service)):
```go
// Services receive repositories + external clients
type SongService struct {
  repo  *repository.SongRepository
  perfRepo *repository.PerformanceRepository
  // ... other dependencies
}
// Methods handle business logic, return errors explicitly
```

**Dependency Injection**: All services initialized in [NewRouter()](backend/internal/handler/router.go#L36) - inspect this for service construction order

**Error Handling**: Errors propagated as HTTP JSON responses with `error` field. Frontend intercepts at [api/client.ts](frontend/src/api/client.ts) line 44

**Configuration**: Environment variables loaded via [internal/config/Load()](backend/internal/config/config.go) - no default .env required

**Database Migrations**: SQL files in [migrations/](backend/internal/database/migrations) executed in alphabetical order by [RunMigrations()](backend/internal/database/migrations.go#L11)

### Frontend Patterns

**Data Fetching**: React Query for queries, manual Axios for mutations (see [api/client.ts](frontend/src/api/client.ts))

**Page Structure**: Each page in [src/pages](frontend/src/pages) handles routing and delegates to components

**Type Safety**: All API responses typed via [types.ts](frontend/src/api/types.ts) - generate types from backend, don't guess

### Database Schema

**Core Tables**:
- `singers`: YouTube channel IDs as primary keys (string)
- `songs`: UUID, unique on (name + original_artist)
- `performances`: Links songs to streams with timestamp ranges (stream_id + song_id + start/end seconds)
- `streams`: YouTube video metadata with JSONB columns for Holodex/Comment analysis results
- `song_itunes`: iTunes track lookup (one song → multiple tracks by country)

**Key Columns**:
- `streams.holodex_data` (JSONB): Raw Holodex response cached
- `streams.comment_data` (JSONB): Comment-parsed songs cached
- `streams.is_processed`: Flag for UI filtering

## Critical Workflows

### Adding a New External API

1. Create client in `pkg/{service_name}/` (e.g., `pkg/youtube/`)
2. Initialize in [NewRouter()](backend/internal/handler/router.go#L36)
3. Inject into relevant service
4. Add endpoints in [setupRoutes()](backend/internal/handler/router.go#L82)
5. Update [frontend/src/api/types.ts](frontend/src/api/types.ts) with response types
6. Call from [frontend/src/api/client.ts](frontend/src/api/client.ts)

### Database Changes

1. Create timestamped SQL migration in [backend/internal/database/migrations/](backend/internal/database/migrations)
   - Format: `NNN_description.sql` (e.g., `007_add_user_table.sql`)
2. Migration runs automatically on server startup
3. Update Go models in [internal/models/models.go](backend/internal/models/models.go)
4. Create repository methods in [internal/repository/](backend/internal/repository)

### Frontend State Management

- **Server State**: React Query (caching, background sync)
- **UI State**: Zustand stores (modals, filters, pagination)
- **Use TanStack Query hooks**: `useQuery`, `useMutation` with proper error handling

## Build & Run

### Backend
```bash
cd backend
go run ./cmd/server/main.go  # Requires .env or environment variables
```

### Frontend
```bash
cd frontend
npm install
npm run dev      # Vite dev server (HMR enabled)
npm run build    # Production build
npm run lint     # ESLint check
```

### Docker
```bash
cd docker
docker-compose up  # PostgreSQL only (backend/frontend services commented out)
```

## Project-Specific Notes

- **Timestamps**: All stored in UTC seconds (integer). Frontend converts for display.
- **Unicode/CJK**: Full UTF-8 support. Japanese readings stored in hiragana for sorting (see `name_reading` fields)
- **Idempotency**: Holodex/YouTube syncs check hashes to avoid re-processing unchanged data
- **Soft Deletes**: Use `is_hidden` flag on streams; no hard deletes from UI
- **AI Integration**: NormalizationService uses Groq API for typo correction - expensive, cache results
- **Rate Limiting**: Implement backoff for external APIs (see [pkg/ratelimit](backend/pkg/ratelimit) pattern)

## Testing & Debugging

- Check `/api/health` endpoint for server readiness
- Frontend TypeScript strict mode: all types must be explicit
- Database connections: PostgreSQL default is `postgres:postgres@localhost:5432/setori`
- Missing env vars: Fall back to safe defaults (see [config.go](backend/internal/config/config.go))
