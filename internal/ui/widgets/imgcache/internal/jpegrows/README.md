This is the Go 1.27.1 `image/jpeg` decoder (BSD-3-Clause, see LICENSE), adapted
locally because the standard API cannot decode into caller storage or release
MCU rows. No module or native library dependency is added.

The Huffman, dequantization and IDCT algorithms are unchanged. Local changes:

- `scan.go`: reject non-baseline, non-interleaved, RGB/CMYK or repeated scans
  before pixel allocation; allocate two MCU rows; wrap block Y coordinates;
  call the consumer after each completed MCU row.
- `rows.go`: expose the ring as an RGBA64Image with original image bounds.
- `reader.go`: add consumer state, remove public Decode/DecodeConfig and format
  registration; fall back if color metadata follows the scan; replace the
  inaccessible image/internal helper with image/draw.

The caller falls back to the standard library on ErrUnsupported. No partial
image is published on error. Progressive/CMYK code is retained from upstream to
keep future upstream diffs easy to audit, but the scan gate prevents its use.
When updating Go, compare these files with src/image/jpeg and run the pixel,
malformation, allocation and corpus tests before adopting decoder changes.

Test JPEGs in testdata are unmodified Go 1.27.1 src/image/testdata files under
the same license. They cover restart markers, subsampling variants, grayscale,
RGB/CMYK and progressive fallbacks.
