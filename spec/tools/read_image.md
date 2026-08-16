# read_image

Read an image file and return its base64 content plus metadata, for multimodal consumption by vision-capable models.

## When to Use

Use `read_image` when the agent needs to **see** an image file — for example, after taking a screenshot with `execute_bash`, or when analyzing image assets in the workspace. The result contains the image content as base64 (ready for `data:image/<mime>;base64,<data>`) and metadata (`mime_type` / `format` / `width` / `height` / `size_bytes`) that describe the image.

`read_file` remains the tool for **text** files; it rejects binary files and is unaffected by this tool.

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "read_image",
    "input": {
        "file_path": "screenshots/foo.png"
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `file_path` | `string` | **Yes** | Path to the image file to read. Subject to workspace sandbox. |

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "file_path": "/workspace/screenshots/foo.png",
        "mime_type": "image/png",
        "format": "png",
        "width": 1920,
        "height": 1080,
        "size_bytes": 234567,
        "base64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB..."
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `file_path` | `string` | Absolute path resolved by the sandbox. **Note:** unlike `read_file`, this is the resolved absolute path, not the input path. |
| `mime_type` | `string` | MIME type derived from magic bytes, e.g. `image/png`. |
| `format` | `string` | MIME subtype (`png`/`jpeg`/`gif`/`webp`/`bmp`). Always `jpeg` — never `jpg`. |
| `width` | `integer` | Pixel width. `0` for `webp`/`bmp` in v1. |
| `height` | `integer` | Pixel height. `0` for `webp`/`bmp` in v1. |
| `size_bytes` | `integer` | Raw file size in bytes. |
| `base64` | `string` | Raw file content encoded as base64. |

## Format Detection

The format is detected by **magic bytes**, never by file extension:

| Format | Magic bytes |
|---|---|
| PNG | `89 50 4E 47 0D 0A 1A 0A` |
| JPEG | `FF D8 FF` |
| GIF | `GIF87a` / `GIF89a` |
| WebP | `RIFF....WEBP` |
| BMP | `BM` |

A JPEG file named `photo.jpg` still returns `format: "jpeg"`.

## Safety Limits

| Limit | Value | Behavior on Exceed |
|---|---|---|
| Max image size | 5 MB (raw bytes) | Rejected: `image file is too large (<N> bytes, max <M> bytes)...` |
| Unsupported format | — | Rejected: `unsupported image format: <path> (supported: png, jpeg, gif, webp, bmp)` |

The max size is configurable via the `--max-image-size` server flag (bytes). The file is `stat`-ed **before** reading; oversized files are rejected without being read. `svg` is not supported in v1 — it is plain text and can be read with `read_file`.

## Error Cases

| Condition | Error message |
|---|---|
| File not found | `file not found: <path>` |
| Path is a directory | `path is a directory: <path>` |
| Unsupported format | `unsupported image format: <path> (supported: png, jpeg, gif, webp, bmp)` |
| File too large | `image file is too large (<N> bytes, max <M> bytes). Please compress the image and try again` |
| Path outside workspace | `security error: path "..." is outside of workspace root "..."` |

## Examples

### Read a screenshot

```json
{"id":"1","method":"tool","tool":"read_image","input":{"file_path":"screenshots/home.png"}}
```

### Read a JPEG (extension mismatch is fine)

```json
{"id":"2","method":"tool","tool":"read_image","input":{"file_path":"assets/logo.jpg"}}
```
