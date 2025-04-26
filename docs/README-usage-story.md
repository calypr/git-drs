
# Usage story

This story tracks a propose usage pattern for an analyist interactive with 
a Calypr project using the git plugin. In this case, the plugin is named git-drs, although 
that may be modified in the future.

In this case, there is an existing project defined at github.com/ohsu-comp-bio/test-project.git

## Install plugin configuratin plugin

```bash
$ git drs install
```

## clone a project

```bash
$ git clone git@github.com:ohsu-comp-bio/test-project.git
```

At this point no file have been downloaded. A hidden folder tracks all document references.

## List document references

This lists all document references added to a project. A document reference includes:
 - The name of the file
 - The project relative path of the file
 - The size of the file
 - File identifiers, such as etags, MD5 or SHA256
 - An array of locations. This could include multiple URLs, file paths or other download methods

```bash
$ git drs list
R ./my-data/sample1.bam
R ./my-data/sample1.bam.bai
L ./my-data/sample2.bam
M ./my-data/sample2.bam.bai
U ./my-data/sample1.vcf
G ./my-data/sample1.txt
```

Codes are:
+-----+-------+
|R | Remote |
|L | Local  |
|M | Modified |
|U | Untracked |
|G | Git tracked file|
+--+---------+


Download a file

```bash
$ git drs ls ./my-data/simple1.bam
R ./my-data/simple1.bam
$ git drs pull ./my-data/sample1.bam
L ./my-data/simple1.bam
```

Add a local file
```bash 
$ git drs add ./my-data/simple1.vcf
M ./my-data/simple1.vcf
```
In this version, the file is moved to a `Modified` state. The file will be uploaded to the default bucket for the project on `push`, at which point it will be changed to a `Local` state.

Add a local file to a non default bucket
```bash
$ git drs add ./my-data/simple1.vcf -r alt-bucket
M ./my-data/simple1.vcf
```


Add a local file that is a symbolic link to a shared folder
```bash
$ git drs add ./my-data/simple1.vcf -l /mnt/shared/results/simple1.vcf
L ./my-data/simple1.vcf
```
In this version, the file is added as a reference, but not pushed to a project repository. A 
symlnk to the actual file is added to the project folder and the state is changed to Local.

Add an existing S3 resource to the project
```bash
$ git drs add ./my-data/simple1.vcf --s3 forterra/my-bucket/results/simple1.vcf
L ./my-data/simple1.vcf
```
This moves the file from the `Untracked` state to `Remote` state.

Push
move any files in the modified state to remote repositories
```bash
$ git drs ls ./data/
R ./my-data/sample1.bam
R ./my-data/sample1.bam.bai
M ./my-data/simple1.vcf
$ git drs push
L ./my-data/simple1.vcf
```

# Remote management

List repositories that are associated with project
```bash
$ git drs remote list
default gen3 calypr.ohsu.edu compbio/my-project
alternate s3 rgw.ohsu.edu MyLab
arc-local local arc.ohsu.edu,*.arc.ohsu.edu /mnt/shared/dir/
anvil drs anvilproject.org
```

The output pattern is resource name, interface type, hostname, remote base path, with 
default being the name of the default storage resource.
In the case of S3 objects, hostname is the server URL. For local storage entries, 
the list of host names (comma delimited with * wildcards) is host names where the local file
storage should be valid


Add remote server
```bash
$ git drs remote add gen3 compbio/my-project
```


# DRS info

```bash
$ git drs info ./my-data/simple1.vcf
```

Should return something like:
```json
{
  "id": "drs://example.org/12345",
  "name": "simple1.vcf",
  "self_uri": "drs://example.org/12345",
  "size": 2684354560,
  "created_time": "2023-01-15T12:34:56Z",
  "updated_time": "2023-06-20T14:22:10Z",
  "version": "1.0",
  "mime_type": "application/octet-stream",
  "checksums": [
    {
      "type": "md5",
      "checksum": "1a79a4d60de6718e8e5b326e338ae533"
    }
  ],
  "access_methods": [
    {
      "type": "https",
      "access_url": {
        "url": "https://example.org/data/HG00096.bam"
      },
      "region": "us-east-1"
    }
  ],
  "description": "BAM file for sample HG00096 from the 1000 Genomes Project",
  "aliases": [
    "1000G_HG00096_bam"
  ],
  "contents": []
}
```


## Storage 
All DRS records will be in a folder under git top level directory in a folder named `.drs`

```bash
$ find .drs/ 
./drs/my-data/sample1.bam
./drs/my-data/sample1.bam.bai
./drs/my-data/sample2.bam
./drs/my-data/sample2.bam.bai
./drs/my-data/sample1.vcf
./drs/my-data/sample1.txt
```