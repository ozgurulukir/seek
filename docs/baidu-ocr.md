# Baidu OCR support

## Finding

Baidu’s documented OCR REST API cannot be configured directly with seek’s current `OCRClient`. The current client requires an OpenAI-compatible vision `/chat/completions` endpoint. Baidu’s standard OCR endpoint uses a different URL, authentication scheme, form body, image representation, and response shape. Therefore, use an OpenAI-compatible adapter/proxy in front of Baidu OCR, or add a native Baidu client to seek; changing only `ocr.base_url`, `ocr.api_key`, or `ocr.model` is insufficient.

This conclusion is based on the current repository code and Baidu’s official standard OCR API documentation.

## What seek currently sends

- `OCRConfig` exposes `enabled`, `base_url`, `api_key`, `model`, and `max_tokens`; an unset OCR URL/key falls back to the embedding provider, the model defaults to `qwen-vl-ocr`, and the token limit defaults to 2048 ([config.go](../internal/config/config.go#L100), [config.go](../internal/config/config.go#L294)).
- `NewExtractor` constructs `OCRClient` only when `CanUseOCR()` allows it: OCR must be enabled with a key, and `privacy.offline_only` additionally requires a numeric loopback endpoint ([indexer.go](../internal/indexer/indexer.go#L112), [config.go](../internal/config/config.go#L228)).
- `OCRClient` appends `/chat/completions` after normalizing a trailing slash, sends `Content-Type: application/json` and `Authorization: Bearer <api_key>`, and posts non-streaming JSON containing `model`, `messages`, `temperature`, `max_tokens`, a text instruction, and an `image_url` part ([ocr.go](../internal/embed/ocr.go#L35)).
- It only accepts `choices[0].message.content`; an OCR response with no `choices` is an error ([ocr.go](../internal/embed/ocr.go#L57), [ocr.go](../internal/embed/ocr.go#L111)).
- For scanned PDFs, seek renders each page to PNG and passes a `data:image/png;base64,...` data URI to the client ([builtin.go](../internal/extractor/builtin/builtin.go#L136), [builtin.go](../internal/extractor/builtin/builtin.go#L169)).

## What Baidu’s official OCR API requires

Using Baidu’s documented standard endpoint as the comparison:

- Endpoint: `POST https://aip.baidubce.com/rest/2.0/ocr/v1/general_basic`, with `access_token` as a URL parameter—not `/chat/completions` and not a Bearer header ([Baidu standard OCR request](https://cloud.baidu.com/doc/OCR/s/zk3h7xz52#%E8%AF%B7%E6%B1%82%E8%AF%B4%E6%98%8E)).
- Authentication: the access token is obtained with the application API Key and Secret Key via `grant_type=client_credentials`; Baidu documents token validity as 30 days ([Baidu access-token instructions](https://ai.baidu.com/ai-doc/OCR/skruaza7j#2-%E8%8E%B7%E5%8F%96-access-token), [Baidu OCR calling instructions](https://cloud.baidu.com/doc/OCR/s/Ck3h7y2ia#%E8%B0%83%E7%94%A8%E6%96%B9%E5%BC%8F)). Seek has no OCR Secret Key field or token-refresh flow.
- Body: `Content-Type: application/x-www-form-urlencoded`; the image is sent as an `image` form value containing URL-encoded base64. Baidu’s calling instructions say the base64 value must omit the `data:image/...;base64,` header ([Baidu request format and limits](https://cloud.baidu.com/doc/OCR/s/Ck3h7y2ia#%E8%AF%B7%E6%B1%82%E6%A0%BC%E5%BC%8F), [Baidu standard OCR request example](https://cloud.baidu.com/doc/OCR/s/zk3h7xz52#%E8%AF%B7%E6%B1%82%E8%AF%B4%E6%98%8E)).
- Response: Baidu returns JSON with `words_result_num` and a `words_result` array whose entries contain `words`; it does not return OpenAI `choices[].message.content` ([Baidu standard OCR response](https://cloud.baidu.com/doc/OCR/s/zk3h7xz52#%E8%BF%94%E5%9B%9E%E8%AF%B4%E6%98%8E)).

## Adapter requirements and limitations

An adapter must expose the shape seek already expects:

1. Accept `POST /chat/completions` with seek’s JSON request.
2. Extract the image from `messages[].content[].image_url.url`, strip the data-URI prefix, and URL-encode the raw base64 into Baidu’s `image` form field.
3. Obtain/cache/refresh Baidu’s `access_token` using API Key + Secret Key, and pass it as the Baidu URL parameter.
4. Call the selected Baidu OCR endpoint and translate `words_result[].words` into `choices[0].message.content` (for example, joining recognized lines with newlines).

The adapter must also enforce the selected Baidu endpoint’s input limits. The current Baidu calling guide states: PNG/JPG/JPEG/BMP/TIFF/PNM/WebP, base64 plus URL encoding no larger than 4 MiB, shortest edge at least 15 px, and longest edge at most 4096 px ([Baidu OCR calling limits](https://cloud.baidu.com/doc/OCR/s/Ck3h7y2ia#%E8%AF%B7%E6%B1%82%E9%99%90%E5%88%B6)). The standard endpoint page separately documents an 8 MiB encoded-image limit for `general_basic`; verify the limit for the exact endpoint/account version used ([Baidu standard OCR parameters](https://cloud.baidu.com/doc/OCR/s/zk3h7xz52#%E8%AF%B7%E6%B1%82%E5%8F%82%E6%95%B0)).

No repository files other than this note were changed.
