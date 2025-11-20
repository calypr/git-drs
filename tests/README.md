## Tests

Tests are currently run manually. First specify the profile that you want to use in the environment variables then run the tests, ex:

These tests use the new moved calypr_admin utility. You will have to build a calypr_admin venv in order for this to work since an end to end test requires giving yourself the proper permissions to do these operations on the calypr platform

Make sure you have the most up to date git-drs and build from source, ex: `go build . ; go install .`

```
python -m venv venv ; source venv/bin/activate
pip install -e [wherever your calypr_admin directory is]

```

```
cd to where your git-drs directory is
export GIT_DRS_PROFILE="YOUR_PROFILE_NAME_GOES_HERE"
export GH_PAT="YOUR_SOURCE_GH_P_ACCESS_TOKEN_GOES_HERE"
export PATH=$PATH:<path/to/git-drs/build>
go test ./...
```

We are currently using source.ohsu.edu so you will have to generate a source.ohsu.edu personal access token if you havent' already.
