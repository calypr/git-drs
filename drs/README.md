

DRS OpenAPI definition

```yaml
type: object
required:
  - id
  - self_uri
  - size
  - created_time
  - checksums
properties:
  id:
    type: string
    description: An identifier unique to this `DrsObject`
  name:
    type: string
    description: |-
      A string that can be used to name a `DrsObject`.
      This string is made up of uppercase and lowercase letters, decimal digits, hyphen, period, and underscore [A-Za-z0-9.-_]. See http://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap03.html#tag_03_282[portable filenames].
  self_uri:
    type: string
    description: |-
      A drs:// hostname-based URI, as defined in the DRS documentation, that tells clients how to access this object.
      The intent of this field is to make DRS objects self-contained, and therefore easier for clients to store and pass around.  For example, if you arrive at this DRS JSON by resolving a compact identifier-based DRS URI, the `self_uri` presents you with a hostname and properly encoded DRS ID for use in subsequent `access` endpoint calls.
    example:
      drs://drs.example.org/314159
  size:
    type: integer
    format: int64
    description: |-
      For blobs, the blob size in bytes.
      For bundles, the cumulative size, in bytes, of items in the `contents` field.
  created_time:
    type: string
    format: date-time
    description: |-
      Timestamp of content creation in RFC3339.
      (This is the creation time of the underlying content, not of the JSON object.)
  updated_time:
    type: string
    format: date-time
    description: >-
      Timestamp of content update in RFC3339, identical to `created_time` in systems
      that do not support updates.
      (This is the update time of the underlying content, not of the JSON object.)
  version:
    type: string
    description: >-
      A string representing a version.

      (Some systems may use checksum, a RFC3339 timestamp, or an incrementing version number.)
  mime_type:
    type: string
    description: A string providing the mime-type of the `DrsObject`.
    example:
      application/json
  checksums:
    type: array
    minItems: 1
    items:
      $ref: './Checksum.yaml'
    description: >-
      The checksum of the `DrsObject`. At least one checksum must be provided.

      For blobs, the checksum is computed over the bytes in the blob.

      For bundles, the checksum is computed over a sorted concatenation of the
      checksums of its top-level contained objects (not recursive, names not included).
      The list of checksums is sorted alphabetically (hex-code) before concatenation
      and a further checksum is performed on the concatenated checksum value.

      For example, if a bundle contains blobs with the following checksums:

      md5(blob1) = 72794b6d

      md5(blob2) = 5e089d29

      Then the checksum of the bundle is:

      md5( concat( sort( md5(blob1), md5(blob2) ) ) )

      = md5( concat( sort( 72794b6d, 5e089d29 ) ) )

      = md5( concat( 5e089d29, 72794b6d ) )

      = md5( 5e089d2972794b6d )

      = f7a29a04
  access_methods:
    type: array
    minItems: 1
    items:
      $ref: './AccessMethod.yaml'
    description: |-
      The list of access methods that can be used to fetch the `DrsObject`.
      Required for single blobs; optional for bundles.
  contents:
    type: array
    description: >-
      If not set, this `DrsObject` is a single blob.

      If set, this `DrsObject` is a bundle containing the listed `ContentsObject` s (some of which may be further nested).
    items:
      $ref: './ContentsObject.yaml'
  description:
    type: string
    description: A human readable description of the `DrsObject`.
  aliases:
    type: array
    items:
      type: string
    description: >-
      A list of strings that can be used to find other metadata
      about this `DrsObject` from external metadata sources. These
      aliases can be used to represent secondary
      accession numbers or external GUIDs.
```