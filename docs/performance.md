# Performance Notes

The provider is lightweight: it serializes Pulumi inputs and calls the Osano REST APIs. There are still a few considerations when running it at scale.

## Throughput

- Osano enforces rate limits per API key. Batch multiple consent actions into a single `Consent` resource when possible to minimize calls.
- Both Osano APIs share one HTTP client with the same retry policy. A request is retried at most three times, honoring `Retry-After` (capped at one minute per wait) and otherwise waiting 1, 2, then 4 seconds:

  | Request | Retried after |
  | --- | --- |
  | GET: every `get*` and `checkConsent` function, `pulumi refresh`, and publication polling | A dropped connection, `429`, or `5xx` |
  | PATCH and DELETE: configuration and rule updates, rule deletes | `429` or `5xx` |
  | POST: configuration, rule, and consent creates, `sendSubjectCode`, `verifySubjectCode` | `429` only. Osano documents no idempotency keys, so a `5xx` may already have been processed and a retry could create a duplicate |
  | POST: the publish request | `429` or `5xx`. A duplicate publish gets `409`, which the provider joins |

- Osano runs one publication per configuration at a time, queues at most 300 configurations per account, and publishes in batches of up to 250 per 30 minutes. A pipeline that publishes many configurations in one update can wait behind these limits; stagger those updates or allow longer `customTimeouts`.
- Separate workloads into distinct stacks (for example, `consents-eu`, `consents-us`) to avoid throttling large previews.

## Latency

- The default HTTP timeout is 60 seconds per request attempt (at most 3600). Override it with `osano:requestTimeoutSeconds` or, when that is unset, `OSANO_API_TIMEOUT_SECONDS`. A retried request waits for each attempt separately.
- The Pulumi engine runs independent resources in parallel (`pulumi up --parallel` controls the limit). Use `dependsOn` only where ordering matters, such as a `CookieConsentPublication` that must wait for its rules.
- `CookieConsentPublication` waits for Osano to finish publishing, which can take several minutes. It honors the resource's create/update `customTimeouts` and stops after 20 minutes when none is set; the examples set 20 minutes explicitly. While a configuration still reports its previous publication's error, the provider keeps polling until that deadline.

## State Size

- Each `osano:index:Consent` resource stores its inputs plus `consentId`, `lastSynced`, and, for a GPC consent, the derived `gpcActions`. Keep attributes compact to avoid bloating state snapshots.
- Use stack outputs sparingly; export aggregated values instead of large payloads.

## Preview Optimization

During `pulumi preview`, resources make no outbound HTTP calls: a create preview shows the inputs with server-assigned outputs unknown, and an update preview carries the prior outputs forward. Functions (invokes) are different: Pulumi runs them during preview, update, and refresh, so every `get*` lookup, and any `sendSubjectCode` or `verifySubjectCode` call, contacts Osano on each run.

The list functions follow Osano's cursor pagination, so one call can make several requests (a list that pages more than 1000 times fails rather than looping):

| Function | Page size | Returns |
| --- | --- | --- |
| `getCookieConsentConfigs` | 1000 | Every match, or the first `maxResults` |
| `getCookieConsentRules` | 500 | Every matching rule of the configuration, or the first `maxResults` |
| `getCookieConsentAuditLog` | 200 | The newest 200 events by default; set `maxResults` (`0` returns every match) |

Narrow them with their filters (for example `configIds`, `eventTypes`, and a date range for the audit log) in programs that run often. Response bodies are read up to 8 MiB.
