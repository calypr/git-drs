`materialize-standalone/main.go` exists because `git drs add-ref` cannot create the required DRS objects.

`add-ref` consumes an object that already exists on a DRS server:

```bash
git drs add-ref <existing-drs-id> <local-path>
```

It creates a local pointer to that object, but it does not define or modify the object’s:

- checksum and size;
- HTTPS access method;
- Globus access method;
- multiple access methods;
- intentionally invalid Globus path.

The standalone fixtures specifically require four different server-side configurations:

| Fixture | Required metadata |
| --- | --- |
| `globus-only.bin` | Globus only |
| `https-globus.bin` | HTTPS and Globus |
| `https-only.bin` | HTTPS only |
| `broken-globus.bin` | working HTTPS and broken Globus |

The main blocker is `https-globus.bin`: the current CLI has no command that creates one DRS object with both access methods. `add-url` creates local metadata for one URL, while `add-ref` expects the complete remote object to exist already.

Therefore, the helper directly creates local DRS metadata and pointer files using git-drs’s existing internal packages. `git drs push` can then register those records.

Could `add-ref` be used instead? Yes—but only after an administrator or another script has already created all four specialized DRS objects on the server. The workflow would become:

```bash
git drs add-ref <globus-only-drs-id> globus-only.bin
git drs add-ref <https-globus-drs-id> https-globus.bin
git drs add-ref <https-only-drs-id> https-only.bin
git drs add-ref <broken-globus-drs-id> broken-globus.bin
```

That merely moves the metadata-construction problem elsewhere.

Architecturally, the Go helper is test scaffolding, not a new user-facing git-drs command. A better long-term solution would be a supported import/registration command that accepts a DRS object manifest. Until that exists, the helper is the smallest deterministic way to create these four test conditions.