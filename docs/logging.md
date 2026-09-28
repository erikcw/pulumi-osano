# Logging

## Provider Logs

- The provider does not log request bodies. Sensitive payloads stay in memory until they are sent to Osano.
- Provider errors include the HTTP status code and up to 2 KiB of the response body Osano returned (`osano api error: status=400 body=...`), which is usually enough to identify a validation or authorization failure. A request that gets no response reports the method and host only (`Osano API request failed: GET https://api.osano.com: ...`), never the path or query, which can hold a session ID. Errors from `sendSubjectCode` and `verifySubjectCode` withhold the body, because it can echo the subject's email address or phone number.
- The provider does not implement dedicated `OSANO_LOG_HTTP` or `OSANO_PROVIDER_DEBUG` flags. Use Pulumi's verbose logging instead:

```bash
pulumi up --logtostderr --logflow -v=9 2> pulumi-debug.log
```

Verbose logs can contain configuration values and resource inputs. Redact API keys, subject identifiers, verification codes and sessions, and the publication `webhookUrl` before sharing them.

The provider reports conditions that do not stop a deployment as Pulumi warnings in the normal `pulumi preview` and `pulumi up` output; no verbose logging is needed to see them:

- an environment variable such as `OSANO_API_KEY` that is set while the stack configures the matching `osano:` key (the stack configuration is used), the deprecated `ucApiKey` and `ucBaseUrl` keys, and an `OSANO_API_TIMEOUT_SECONDS` value that is not a whole number from 1 to 3600;
- `CookieConsentConfig` configuration problems such as unknown or deprecated configuration keys;
- a `CookieConsentConfig` adopted after its create request failed with a server error, and a `CookieConsentPublication` still waiting for Osano to start a new publication behind the previous publication's error;
- a `Consent` whose subject Osano reports without consent during `pulumi refresh` (the resource is kept), and the deprecated `referenceType: anonymous`.

## Correlating with Osano

For Unified Consent, add a custom attribute (for example `attributes["pulumiDeploymentId"]`) to each `Consent` so you can correlate Pulumi deployments with consent records in Osano.

For Cookie Consent, `getCookieConsentAuditLog` returns Osano's audit events with their type (for example `cmp.configPublished` or `cmp.configUpdated`), actor, and timestamp. Match a publication or configuration change against the time of the Pulumi update that made it, or find edits made in the Osano dashboard.

Every request the provider sends identifies it with the User-Agent `pulumi-osano/<version>` (for example `pulumi-osano/0.3.0`).

## Troubleshooting Checklist

1. Re-run the failing command with `--logtostderr --logflow -v=9` and capture the output.
2. Record the HTTP status and response body from the provider error, the approximate time of the request, and the provider version (`pulumi plugin ls`), and include them in support tickets.
3. Delete the debug log once the issue is resolved.
