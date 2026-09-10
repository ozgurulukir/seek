# `seek` OCR sağlayıcı uyumluluğu

Araştırma tarihi: 2026-09-10

Bu not, `seek` kodunun bugün beklediği OCR sözleşmesi ile sağlayıcıların birinci taraf dokümanlarını karşılaştırır. Amaç, “yerel OCR” ile “OpenAI uyumlu vision sunucusu” kavramlarını ayırmak ve Baidu OCR değerlendirmesini düzeltmektir.

## `seek` gerçekte ne bekliyor?

`seek` OCR istemcisi:

- `POST {ocr.base_url}/chat/completions` çağrısı yapar.
- `Authorization: Bearer <ocr.api_key>` ve JSON gövdesi kullanır.
- Görüntüyü `messages[].content[]` içinde `image_url.url` alanına `data:image/png;base64,...` data URI olarak koyar.
- `stream: false` ve varsayılan `max_tokens: 2048` gönderir; limit `ocr.max_tokens` ile değiştirilebilir.
- Sonucu `choices[0].message.content` alanından okur.

Bu davranış [`internal/embed/ocr.go`](../internal/embed/ocr.go#L69-L122) içinde sabittir. OCR istemcisi OCR etkin ve API anahtarı mevcut olduğunda oluşturulur; `privacy.offline_only` açıkken yalnızca numeric loopback URL’leri (`127.0.0.0/8`, `::1`) kabul edilir ([`internal/indexer/indexer.go`](../internal/indexer/indexer.go#L112-L120), [`internal/config/config.go`](../internal/config/config.go#L222-L272)). Bu sınır seek’in doğrudan hedefini kısıtlar; yerel OCR sunucusunun kendi dışarı aktarma davranışı güven sınırının dışındadır. `ocr.base_url`, `ocr.api_key` ve `ocr.model` boş bırakılırsa embedding sağlayıcısından/varsayılan modelden doldurulur ([`internal/config/config.go`](../internal/config/config.go#L294-L307)).

Bu nedenle “doğrudan çalışır” aşağıdaki iki koşulu birlikte ifade eder:

1. Sunucu `POST /v1/chat/completions` veya `/chat/completions` üzerinden görüntülü chat kabul ediyor olmalı.
2. Seçilen model, `seek` tarafından gönderilen genel “metni aynen çıkar” istemini anlayıp metni `message.content` olarak döndürebilmeli.

İlk koşul protokol uyumluluğudur; ikinci koşul model uyumluluğudur. Aşağıdaki “doğrudan” değerlendirmesi birinci taraf dokümanlarına dayalı protokol değerlendirmesidir; gerçek donanım/model kombinasyonu için ayrıca küçük bir smoke test gerekir. `⚠️*` satırları yalnızca transport adayıdır; exact runtime/model/prompt smoke test’i zorunludur.

## Kısa sonuç

| Seçenek | `seek` ile doğrudan? | Veri nerede işlenir? | Değerlendirme |
|---|---|---|---|
| Ollama + `glm-ocr` | Evet, en güçlü ilk aday | Yerelde | OCR’a özel model + OpenAI uyumlu vision endpoint |
| Ollama + `qwen3-vl` / `gemma3` | Evet, protokol düzeyinde | Yerelde | Genel VLM; OCR kalitesi prompt ve modele bağlı |
| vLLM + desteklenen vision/OCR modeli | ⚠️* | Yerelde | Transport uyumlu; exact model/chat-template smoke test gerektirir |
| llama.cpp / `llama-server` + multimodal GGUF | ⚠️* | Yerelde | Transport uyumlu; model ve `mmproj` eşleşmeli |
| LM Studio + yerel vision modeli | ⚠️* | Yerelde | Transport uyumlu; yüklenen model image input desteklemeli |
| PaddleOCR / PaddleOCR-VL klasik pipeline | Hayır, mevcut haliyle | Yerelde | Native OCR API/CLI; OpenAI chat endpoint’i değildir |
| RapidOCR | Hayır, mevcut haliyle | Yerelde | Python/ONNX kütüphanesi; OpenAI chat endpoint’i değildir |
| Tesseract | Hayır, mevcut haliyle | Yerelde | Yerel OCR motoru/CLI; OpenAI chat endpoint’i değildir |
| Baidu OCR public cloud API | Hayır | Baidu cloud | Endpoint, auth, body ve response şekli farklı |
| Baidu OCR offline SDK/private deployment | Hayır, mevcut haliyle | Yerel cihaz veya özel ağ | Gerçek yerel seçenek; SDK/native entegrasyon gerekir |

## 1. Doğrudan çalışabilecek yerel seçenekler

### Ollama + `glm-ocr` — önerilen ilk deneme

Ollama’nın resmi model kütüphanesi `glm-ocr` modelini “multimodal OCR model” olarak tanımlar; modelin metin, tablo ve şekil tanıma kullanımını gösterir ve Ollama ile vLLM üzerinde çalıştırılabildiğini belirtir ([Ollama `glm-ocr` modeli](https://ollama.com/library/glm-ocr)).

Ollama’nın resmi OpenAI uyumluluk dokümanı `/v1/chat/completions` için vision desteğini ve Base64 görüntü input’unu listeler; aynı dokümanda `qwen3-vl:8b` ile görüntülü örnek bulunur ([Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility)). Ollama’nın resmi sunucu kodu ayrıca hem doğrudan string biçimini hem de `image_url: {url: ...}` biçimini kabul eder; bu, `seek`’in gönderdiği nested `url` yapısıyla uyumludur ([Ollama OpenAI adapter source](https://github.com/ollama/ollama/blob/main/openai/openai.go)).

Örnek `seek` ayarı:

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:11434/v1
  api_key: ollama       # Ollama için gerekli görünüyor, sunucu tarafından yok sayılıyor
  model: glm-ocr
```

Modeli indirme/çalıştırma:

```bash
ollama pull glm-ocr
```

Bu modelin resmi Ollama sayfası metin ve görüntü input’unu, yaklaşık 0.9B parametreli OCR odaklı modeli ve `ollama run glm-ocr` kullanımını listeler ([Ollama `glm-ocr`](https://ollama.com/library/glm-ocr)). Bu nedenle ilk yerel deneme için genel bir VLM’den daha mantıklı adaydır.

Sınırlamalar:

- Modeli ilk kez indirmek ağ erişimi ister; model indikten sonra inference yerel çalışır ([Ollama vision](https://docs.ollama.com/capabilities/vision)).
- `seek`’in generic OCR prompt’u ile modelin resmi “Text recognition” örneği arasında birebir prompt garantisi yoktur; sonuç kalitesi gerçek PDF örnekleriyle ölçülmelidir ([Ollama `glm-ocr`](https://ollama.com/library/glm-ocr)).
- `offline_only: true` ile OCR yalnızca numeric loopback endpoint’lerinde (`127.0.0.0/8`, `::1`) kullanılabilir; environment HTTP proxy’leri de bypass edilir. Hostname çözümlemesine güvenilmez; private/public ağ adresleri local kabul edilmez. Bu, seek’in doğrudan dış endpoint’e bağlanmasını engeller; yerel OCR sunucusunun kendi forwarding davranışı ayrıca güvenilmelidir ([`internal/config/config.go`](../internal/config/config.go#L222-L272), [`internal/embed/ocr.go`](../internal/embed/ocr.go#L28-L45)).

### Ollama + `qwen3-vl` veya `gemma3`

Ollama’nın resmi OpenAI uyumluluk sayfası, görüntülü `/v1/chat/completions` isteğini ve `qwen3-vl:8b` modelini doğrudan örnekler ([Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility)). `qwen3-vl` resmi model sayfası OCR desteğini ve 32 dilde OCR iyileştirmelerini belirtir ([Ollama `qwen3-vl`](https://ollama.com/library/qwen3-vl)). `gemma3` sayfası da 4B, 12B ve 27B varyantlarını multimodal/vision olarak listeler ([Ollama `gemma3`](https://ollama.com/library/gemma3)).

Örnek:

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:11434/v1
  api_key: ollama
  model: qwen3-vl:8b
```

Bunlar `seek` ile protokol düzeyinde doğrudan çalışabilecek genel vision modelleridir. Ancak `glm-ocr` kadar OCR-özelleşmiş değillerdir; uzun taranmış sayfalarda, tablo/kolon düzeninde ve küçük yazıda kaliteyi ayrı test etmek gerekir ([Ollama `qwen3-vl`](https://ollama.com/library/qwen3-vl), [Ollama `gemma3`](https://ollama.com/library/gemma3)).

### vLLM + vision/OCR modeli

vLLM resmi sunucusu OpenAI Chat Completions API’sini uygular ve vision parametrelerini destekler ([vLLM OpenAI-compatible server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/)). Resmi multimodal örnek, `/v1` taban adresini, `image_url: {url: "data:image/...;base64,..."}` biçimini ve cevabın `choices[0].message.content` alanından okunmasını gösterir ([vLLM multimodal OpenAI client](https://docs.vllm.ai/en/stable/examples/generate/multimodal/)). Bu üç nokta `seek`’in HTTP sözleşmesiyle örtüşür.

Örnek sunucu ve config:

```bash
vllm serve <vision-model>
```

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:8000/v1
  api_key: local
  model: <vision-model-id>
```

Sınırlamalar:

- Modelin chat template’i ve multimodal desteği olmalıdır; vLLM her text modelini vision modeline dönüştürmez ([vLLM OpenAI-compatible server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/)).
- Resmi dokümana göre `image_url.detail` desteklenmiyor; `seek` bu alanı göndermediği için bu sınırlama mevcut OCR isteğini etkilemez ([vLLM OpenAI-compatible server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/)).
- Pratikte vLLM daha çok GPU/VRAM ve sunucu kurulumu gerektirir. Uygun modelin exact model id’si ve donanım gereksinimi ayrıca doğrulanmalıdır ([vLLM multimodal examples](https://docs.vllm.ai/en/stable/examples/generate/multimodal/)).

### llama.cpp / `llama-server` + multimodal GGUF

llama.cpp resmi multimodal dokümanı `llama-server`’ın OpenAI uyumlu `/chat/completions` endpoint’i üzerinden görüntü, ses ve video input desteklediğini söyler ([llama.cpp multimodal](https://github.com/ggml-org/llama.cpp/blob/master/docs/multimodal.md)). Sunucu dokümanı da OpenAI uyumlu chat-completions route’unu ve multimodal desteği listeler ([llama-server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)).

Örnek:

```bash
llama-server -hf ggml-org/gemma-3-4b-it-GGUF --port 8080
```

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:8080/v1
  api_key: local
  model: gemma-3-4b-it
```

Yerel GGUF dosyası kullanırken model ve multimodal projector birlikte verilmelidir:

```bash
llama-server -m model.gguf --mmproj mmproj.gguf --port 8080
```

Resmi doküman, OCR modellerinin özel prompt ve input yapısına göre eğitildiğini özellikle belirtiyor; PaddleOCR-VL, GLM-OCR, DeepSeek-OCR, Dots.OCR ve HunyuanOCR için ayrı model notları veriyor ([llama.cpp multimodal OCR notu](https://github.com/ggml-org/llama.cpp/blob/master/docs/multimodal.md#multimodal)). Bu yüzden llama.cpp transport açısından doğrudan uyumludur, fakat OCR-özelleşmiş bir GGUF’nin `seek`’in genel prompt’u ile doğru sonuç vereceği varsayılmamalıdır.

### LM Studio + yerel vision modeli

LM Studio resmi dokümanı OpenAI uyumlu endpoint’ler arasında `/v1/chat/completions`’ı ve chat completions için text/image desteğini listeler; örnek base URL `http://localhost:1234/v1`’dir ([LM Studio OpenAI compatibility](https://lmstudio.ai/docs/developer/openai-compat)). LM Studio’nun yerel sunucu dokümanı sunucunun localhost’ta çalıştırılabildiğini ve yerel modellerin kullanılabildiğini açıklar ([LM Studio local server](https://beta.lmstudio.ai/docs/developer/core/server), [LM Studio local models](https://lmstudio.ai/docs/bionic/models)).

Örnek:

```yaml
ocr:
  enabled: true
  base_url: http://127.0.0.1:1234/v1
  api_key: lm-studio
  model: <loaded-vision-model-id>
```

Bu seçenek `seek` ile doğrudan çalışabilir; şart, LM Studio’da yüklenen modelin görüntü input desteklemesidir. LM Studio’daki cloud veya başka makinedeki remote model seçilirse veri artık yalnızca bu bilgisayarda kalmaz ([LM Studio model locations](https://lmstudio.ai/docs/bionic/models)).

## 2. Yerel olan fakat `seek`’e doğrudan bağlanamayan klasik OCR motorları

Bu araçlar “yerel OCR” tanımına daha sıkı biçimde uyar; fakat bir OpenAI-compatible vision server değildir. Mevcut `seek` `OCRClient`’ı bunların Python fonksiyonunu, CLI çıktısını veya JSON endpoint’ini çağırmaz. Kullanılmaları için ileride native bir `extractor.OCR` implementasyonu ya da ayrı bir HTTP server entegrasyonu gerekir. Bu araştırmanın kapsamında adapter kodu yazılmamıştır.

### PaddleOCR / PP-OCR

PaddleOCR resmi projesi yerel kurulum ve Python/C++/serving seçenekleri sunar; PP-OCR deployment dokümanı Python inference, C++, serving, Paddle-Lite ve ONNX gibi ayrı deployment yolları listeler ([PaddleOCR resmi repo](https://github.com/PaddlePaddle/PaddleOCR), [PP-OCR deployment](https://github.com/PaddlePaddle/PaddleOCR/blob/main/deploy/README.md)). Bu arayüzler `POST /chat/completions` + `choices[0].message.content` sözleşmesi değildir. Sonuç: **PaddleOCR klasik pipeline doğrudan çalışmaz, ancak yerel ve native entegrasyon için güçlü bir adaydır.**

PaddleOCR-VL veya PaddleOCR’un document-VLM ürünleri ayrı değerlendirilmelidir. Projenin resmi README’si bazı yeni document parsing bileşenlerinin OpenAI-compatible serving ve vLLM tabanlı çalışabildiğini belirtir ([PaddleOCR resmi repo](https://github.com/PaddlePaddle/PaddleOCR)). Böyle bir sunucu gerçekten `seek`’in beklediği endpoint ve response şeklini veriyorsa vLLM bölümündeki gibi doğrudan kullanılabilir; yalnızca `PaddleOCR` Python paketini kurmak bunu sağlamaz.

### RapidOCR

RapidOCR resmi reposu bunu çok platformlu, offline deploy edilebilir bir OCR aracı olarak tanımlar; kurulum ve kullanım örneği `RapidOCR()` Python nesnesini doğrudan çağırır ([RapidOCR resmi repo](https://github.com/RapidAI/RapidOCR)). ONNX Runtime, OpenVINO, Paddle, TensorRT ve başka inference engine’leri destekler, fakat bunlar inference backend’leridir; OpenAI chat server sözleşmesi değildir ([RapidOCR inference engines](https://github.com/RapidAI/RapidOCR/blob/main/python/rapidocr/config.yaml)). Sonuç: **yerel çalışır, fakat `seek`’e doğrudan bağlanmaz.**

RapidOCRWeb yerelde Flask tabanlı bir web uygulaması başlatabilir, ancak resmi kullanım örneği `rapidocr_web -ip ... -p ...` ile tarayıcı arayüzüdür; `seek`’in beklediği OpenAI endpoint’i olarak belgelenmemiştir ([RapidOCRWeb resmi repo](https://github.com/RapidAI/RapidOCRWeb)).

### Tesseract

Tesseract resmi dokümanı onu Apache 2.0 lisanslı bir text recognition engine olarak tanımlar ve kullanım/API/CLI dokümantasyonu sağlar ([Tesseract User Manual](https://tesseract-ocr.github.io/tessdoc/), [Tesseract source repository](https://github.com/tesseract-ocr/tesseract)). Bu nedenle veri yerelde tutulabilir, ancak Tesseract görüntülü chat endpoint’i değildir. Sonuç: **doğrudan çalışmaz; native Go/CLI entegrasyonu gerekir.**

Bu motorlar özellikle deterministik OCR, bounding box, dil modeli ve düşük donanım ihtiyacı isteniyorsa değerlidir. Fakat `seek`’in şu anki `OCRClient`’ı bunların yapılandırılmış sonuçlarını alacak bir abstraction sunmuyor; yalnızca dönen metni kullanabilecek `extractor.OCR` arayüzü mevcut ([`internal/extractor/extractor.go`](../internal/extractor/extractor.go#L70-L72)).

## 3. Baidu OCR’ın yeniden değerlendirilmesi

### Baidu public cloud API

Baidu’nun standart OCR API’si `POST https://aip.baidubce.com/rest/2.0/ocr/v1/general` benzeri endpoint’ler kullanır. Resmi dokümanda `access_token` URL parametresi, `application/x-www-form-urlencoded` body ve URL-encoded Base64 `image` alanı gösterilir ([Baidu standard OCR request](https://ai.baidu.com/ai-doc/OCR/vk3h7y58v), [Baidu request format](https://ai.baidu.com/ai-doc/OCR/Ck3h7y2ia)). Bu, `seek`’in JSON + Bearer + `messages[].content[].image_url.url` isteğiyle uyumlu değildir.

Baidu ayrıca Access Token’ın API Key ve Secret Key ile alınacağını ve token’ın varsayılan olarak 30 gün geçerli olduğunu belirtir ([Baidu authentication](https://ai.baidu.com/ai-doc/REFERENCE/Ck3dwjgn3)). `seek`’in `OCRConfig` yapısında Secret Key veya token yenileme alanı/akışı yoktur ([`internal/config/config.go`](../internal/config/config.go#L104-L109)). Sonuç: **Baidu public cloud OCR, yalnızca `ocr.base_url` değiştirerek doğrudan çalışmaz.**

Bu hizmette görüntü Baidu’nun public cloud’una gönderilir; local/offline kabul edilemez ([Baidu OCR public cloud product](https://ai.baidu.com/tech/ocr)).

### Baidu offline SDK ve private deployment

Önceki “Baidu kesinlikle yerel değildir” ifadesi eksikti. Baidu’nun resmi ürün dokümanı ayrı bir OCR offline SDK sunduğunu, yetkilendirme sonrası ağsız ortamda cihaz üzerinde çalışabildiğini ve Windows/Android/iOS/Linux gibi platformları desteklediğini söylüyor ([Baidu OCR offline SDK overview](https://cloud.baidu.com/doc/OCR/s/0ki4d4wf), [Baidu offline SDK product](https://cloud.baidu.com/product/OCR/sdk.html)). Baidu ayrıca OCR’ın yerel sunucu veya özel ağda private deployment biçimlerinin bulunduğunu belirtiyor ([Baidu OCR operation guide](https://cloud.baidu.com/doc/OCR/s/6l24j8vqo), [Baidu OCR product](https://ai.baidu.com/tech/ocr_general/)).

Bu seçenekler gerçekten yerel olabilir; fakat mevcut `seek` için yine de doğrudan değildir:

- Offline SDK, OpenAI-compatible HTTP endpoint değil, platforma/SDK’ye özel entegrasyondur.
- Private deployment’ın `seek` ile doğrudan kullanılabilmesi için Baidu’nun sağladığı servisin `/chat/completions` ve beklenen response shape’i sunması gerekir; resmi dokümanlarda bu uyumluluk gösterilmiyor.
- Offline SDK’de cihaz başına lisans/aktivasyon modeli bulunur ([Baidu offline SDK authorization](https://cloud.baidu.com/doc/OCR/s/vkia5oivy)).

Dolayısıyla Baidu için doğru sınıflandırma şudur: **public cloud API doğrudan uyumsuz ve veriyi dışarı çıkarır; offline SDK/private deployment yerel olabilir ama mevcut `seek` OCR sözleşmesine doğrudan bağlanmaz.**

## Önerilen karar

İlk PoC için:

1. Ollama’da `glm-ocr` çalıştırıp `ocr.base_url: http://127.0.0.1:11434/v1` ile dene.
2. Aynı test görsellerini `qwen3-vl:8b` ile karşılaştır.
3. CPU/RAM sınırı varsa llama.cpp + küçük multimodal GGUF dene; GPU ve throughput önemliyse vLLM’e geç.
4. `offline_only` açıkken OCR endpoint’inin numeric loopback olduğundan emin ol. Uzak, private-network ve hostname endpoint’leri reddedilir; yerel sunucunun kendi forwarding davranışı ayrıca güvenilmelidir.
5. PaddleOCR/RapidOCR/Tesseract/Baidu offline SDK’yi, ancak daha deterministik OCR veya layout/bounding-box ihtiyacı oluşursa native `extractor.OCR` provider’ı olarak ele al.

En düşük entegrasyon maliyetli ve OCR’a en yakın ilk aday: **Ollama + `glm-ocr`**. Bu öneri, sağlayıcının model sayfasındaki OCR odaklı açıklama ile Ollama’nın `seek` formatıyla örtüşen OpenAI-compatible vision endpoint’ine dayanır; gerçek karar için Türkçe/İngilizce taranmış PDF örnekleriyle doğruluk ve latency ölçümü gereklidir ([Ollama `glm-ocr`](https://ollama.com/library/glm-ocr), [Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility)).
