# API Reference

Base URL: `/api/v1`

All responses use the standard envelope:

```json
{
  "success": true,
  "message": "Description",
  "data": {},
  "meta": { "count": 10, "total": 50, "page": 1, "page_size": 50 },
  "timestamp": "2026-04-04T10:30:00Z"
}
```

## Authentication

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| POST | `/auth/login` | No | Login with email + password. Returns JWT tokens. |
| POST | `/auth/register` | No | Register via invite token. |
| POST | `/auth/refresh` | No | Refresh access token. |
| GET | `/auth/me` | Yes | Current user profile. |

## Documents

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/documents` | Yes | All | List documents (filter: `?category_id=`) |
| GET | `/documents/:id` | Yes | All | Document detail + version history |
| POST | `/documents` | Yes | Admin, Company | Upload document (multipart) |
| PUT | `/documents/:id` | Yes | Admin, Company | Update metadata |
| DELETE | `/documents/:id` | Yes | Admin, Company | Archive (soft delete) |
| POST | `/documents/:id/versions` | Yes | Admin, Company | Upload new version |
| GET | `/documents/:id/versions/:v` | Yes | All | Download specific version (download permission) |
| DELETE | `/documents/:id/versions/:v` | Yes | Admin, Company | Delete version (last version cannot be removed) |
| GET | `/documents/:id/preview` | Yes | All | Preview current PDF bytes (download permission) |
| GET | `/documents/:id/versions/:v/preview` | Yes | All | Preview specific PDF version bytes (download permission) |
| GET | `/documents/:id/download` | Yes | All | Download current version (download permission) |
| POST | `/documents/search` | Yes | All | Full-text search (`{"query":"..."}`) |

Document list/search entries include `can_download`. Detail responses include
`permissions: {can_download, can_manage}` alongside `document` and `versions`.
PDF preview returns authenticated `application/pdf` bytes with inline disposition
and `Cache-Control: private, no-store` only with download permission; non-PDF previews return 422.
View-only clients use preview-info and preview-pages to receive PNG pages. Missing/inaccessible
or archived documents return 404. Investors cannot upload or delete versions,
regardless of their grant level. Deleting the current version switches to the
highest remaining version and updates MIME type and size atomically. Version
numbers are never reused. Deleting the last version returns 409; delete the
document instead.

## Categories

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/categories` | Yes | All | List categories (tree structure) |
| POST | `/categories` | Yes | Admin | Create category |
| PUT | `/categories/:id` | Yes | Admin | Update category |
| DELETE | `/categories/:id` | Yes | Admin | Delete (no active documents or subcategories) |

## Permissions

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/permissions/document/:id` | Yes | Admin | List grants for document |
| GET | `/permissions/category/:id` | Yes | Admin | List grants for category |
| GET | `/permissions/user/:id` | Yes | Admin | List all grants held by a user |
| POST | `/permissions` | Yes | Admin | Grant access |
| PUT | `/permissions/:id` | Yes | Admin | Update access level |
| DELETE | `/permissions/:id` | Yes | Admin | Revoke access |

## Q&A

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/qa` | Yes | All | List threads (filter: `?status=`) |
| POST | `/qa` | Yes | All | Create question |
| GET | `/qa/:id` | Yes | All | Thread with messages |
| POST | `/qa/:id/messages` | Yes | All | Post message |
| PATCH | `/qa/:id/status` | Yes | Admin, Company | Change status |

## NDA

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/nda/templates` | Yes | Admin | List templates |
| POST | `/nda/templates` | Yes | Admin | Create template |
| PUT | `/nda/templates/:id` | Yes | Admin | Update template |
| GET | `/nda/status` | Yes | All | Check signing status (`signed`; `exempt: true` when waived) |
| POST | `/nda/sign/:templateId` | Yes | All | Sign NDA |
| GET | `/nda/signatures` | Yes | Admin | List all signatures |
| GET | `/nda/exemptions` | Yes | Admin | List NDA exemptions |
| POST | `/nda/exemptions` | Yes | Admin | Grant exemption (multipart: `user_id`, optional `reason`, optional `file` — externally executed NDA, max 25MB). Re-granting replaces the exemption. |
| DELETE | `/nda/exemptions/:userId` | Yes | Admin | Revoke exemption |
| GET | `/nda/exemptions/:userId/document` | Yes | Admin | Download the uploaded executed NDA |

## Audit

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/audit` | Yes | Admin | List audit log (filter: `?action=&user_id=`) |
| GET | `/audit/document/:id` | Yes | Admin | Audit trail for document |
| GET | `/audit/user/:id` | Yes | Admin | Activity for user |

## Analytics

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/analytics/dashboard` | Yes | Admin, Company | Engagement summary |
| GET | `/analytics/documents/:id` | Yes | Admin, Company | Per-document analytics |
| GET | `/analytics/users/:id` | Yes | Admin, Company | Per-user engagement |
| POST | `/analytics/view-event` | Yes | All | Record view event |

## Branding

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/branding` | Yes | All | Get branding config |
| PUT | `/branding` | Yes | Admin | Update branding (CSS sanitized) |
| DELETE | `/branding` | Yes | Admin | Reset to defaults |
| GET | `/branding/assets/:key` | Yes | All | Get asset binary |
| POST | `/branding/assets/:key` | Yes | Admin | Upload asset (max 2MB) |
| DELETE | `/branding/assets/:key` | Yes | Admin | Delete asset |

Valid asset keys: `logo`, `logo_dark`, `favicon`, `login_background`, `email_header`, `report_header`, `report_footer`

## Watermark

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/watermark` | Yes | Admin | Get watermark config |
| PUT | `/watermark` | Yes | Admin | Update watermark config |
| DELETE | `/watermark` | Yes | Admin | Reset to defaults |

## Users

| Method | Path | Auth | Role | Description |
| --- | --- | --- | --- | --- |
| GET | `/users` | Yes | Admin | List users |
| GET | `/users/:id` | Yes | Admin | Get user |
| PUT | `/users/:id` | Yes | Admin | Update user |
| PUT | `/users/:id/password` | Yes | Admin | Set a user's password |
| DELETE | `/users/:id` | Yes | Admin | Deactivate user |
| POST | `/users/invite` | Yes | Admin | Create invite |

**Set password** (`PUT /users/:id/password`): body `{"password": "...", "current_password": "..."}`.
`password` is required, must be at least 8 characters, and at most 72 bytes (bcrypt's limit).

- **Another user**: `current_password` is ignored. Returns `{"user": {...}}`. Role and active
  state are unchanged, so a disabled account can be given a password before it is re-enabled.
- **Your own account**: `current_password` is required and re-verified. Returns
  `{"user": {...}, "access_token": "...", "refresh_token": "..."}`; store the new tokens.

Errors: `400` for an invalid body, a password that breaks the length rules,
`current password is required`, or `current password is incorrect` (never `401`, which the UI
treats as an expired session); `404` for an unknown user; `403` for non-admins; `429` when the
per-IP limit is exceeded (`DD_LOGIN_RATE_LIMIT`, default 10 per minute, the login budget with its
own counter, because the `current_password` check could otherwise be used to guess passwords).
Wrong `current_password` attempts are logged as warnings. The change is audited as
`user.password_set`; passwords never appear in responses, logs, or audit entries.

Changing a password revokes every refresh token issued to that user before the change: refreshing
returns `401` and they must sign in again (the self-change response carries a fresh pair for the
caller). Access tokens already issued are not revoked and remain valid until they expire (up to
15 minutes). Refresh tokens issued by versions before this feature carry no password version and
are rejected, so users sign in once more after upgrading.

## Health (no auth, no `/api/v1` prefix)

| Method | Path | Description |
| --- | --- | --- |
| GET | `/health` | Basic health check |
| GET | `/ready` | Readiness (SQLite ping) |
| GET | `/version` | Build version info |

### PDF preview and watermarks

Current version routes: `GET /documents/:id/preview` (PDF, download permission),
`GET /documents/:id/preview-info` (view permission), and
`GET /documents/:id/preview-pages/:page` (view permission, PNG).
Historical versions use `/documents/:id/versions/:version/preview`,
`.../preview-info` and `.../preview-pages/:page`. Page numbers start at 1.

Preview info data contains `mode` (`pdf` or `images`), `version_number`, `name`,
and `pages` (`width`, `height` in points; empty in PDF mode). Clients pin the
returned version before retrieving content. Document list/detail/search data
includes `can_download`, computed from current server permissions.

All PDF byte routes require download permission; hiding client controls is not
an authorization check. View-only users receive PNG pages, never a PDF fallback.
In `DD_INVESTOR_ACCESS=all`, ungranted investors have download access but explicit
view-only grants restrict it. Use `DD_INVESTOR_ACCESS=granted` to require grants
for all document access.

Enabled watermarks apply to every PDF page on authorized delivery and are
personalized with JWT identity, document name and UTC date `YYYY-MM-DD`.
Original stored versions are untouched. Other file types are unchanged.
Private outputs use `Cache-Control: private, no-store`; send JWT in the
Authorization header, never in a URL. Access is rechecked before delivery,
including after preparation and when serving cached artifacts.

Preparation errors use the standard JSON error envelope: 413 (input cap),
422 (damaged/encrypted/unsupported PDF), 503 (queue full, `Retry-After: 3`),
504 (preparation timeout). Error paths never return an unmarked PDF when
watermarking is enabled. Existing 404 conventions conceal inaccessible versions.
