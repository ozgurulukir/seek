# Phase 4 — Refactoring Blueprint & Mission Cards

**Tarih:** 2026-09-09 · **Kapsam:** #7 umbrella steps 4-8 · **Durum:** plan — uygulama her kart için ayrı onayla

---

## 0. Mevcut durum (keşif kanıtları)

T1-T3 sonrası temel zemin hazır: `--json` zarfı + `content_kind` (T1.1), `seek mcp` 3-tool stdio server (T1.2),
store 9 dosyaya bölündü (T2), metadata SSOT + fast-field faceting (T3: `tags`/`date`/`author` markdown,
`lang`/`repo` code, `--aggs lang:terms` çalışıyor).

### Saptanan somut mimari borçlar (blueprint'in adresleyeceği)

1. **Sync↔embed kopukluğu:** stop-hook (`cmd/hooks.go:289`) `seek sync --no-lock` ve `seek embed --no-lock`
   **iki ayrı process, iki ayrı `store.Open`** olarak çalıştırıyor. `sync` yeni chunk'lar bırakır, `embed`
   ayrı geçişte toplar — bekleme penceresi + çift WAL lock yönetimi. `SyncCollection` (indexer.go:145)
   embed bilmeyecek şekilde yazılmış; embed tarafı `GetChunksWithoutEmbedding` ile DB'yi yeniden tarar.
2. **Indexer format dispatch** (indexer.go:146): 10 sync fonksiyonu `switch col.Type` ile elle dağıtılıyor;
   markdown/code claude/codex/pdf/images her biri kendi çıkarım + chunk + fast-field desenini tekrarlıyor
   (T3'te metadata yazımını iki yerde elle ekledik: syncMarkdown ve indexCodeFile — desen zaten sızdı).
3. **embed provider yüzeyi:** `Client`/`VLClient`/`OCRClient`/`RerankClient` ayrı tipler, hiçbiri ortak
   arayüzü paylaşmıyor; cmd/embed.go client seçimini `IsMultimodal()` + branch'lerle yapıyor. Yeni provider
   (ör. lokal ollama, vLLM) eklemek cmd katmanına branch eklemek demek.
4. **Extractor neredeyse hazır:** `extractor.Extractor` arayüzü + `builtin`/`xberg` backend'leri + `OCR`
   capability arayüzü mevcut (§6.13 metadata SSOT'u hermes'te kanıtlı). Ancak **markdown/code/scanner
   akışları** (source.ScanMarkdown/ScanCode) bu arayüzün dışında — iki paralel dosya-çıkarım yolu var.

## 1. Hedef mimari (kabuk taslağı)

```
cmd/            → sadece CLI orkestrasyonu (kong), ince tutulmaya devam
internal/pipeline (yeni) → sync+embed tek akış: SyncCollection sonrası "pending chunk" setini
                          doğrudan embed'e geçirir; lock bir kez alınır, db bir kez açılır
internal/indexer→ syncSourceRegistry: collection type → syncHandler kaydı (switch'i Map'e çevir);
                          her handler: scan → extract (Extractor) → chunk → store+FTS+fastfield →
                          döndür: []pendingChunk
internal/embed  → Provider arayüzü: Text/Complete/VL yeteneklerinin ayrı interface'leri;
                          client seçimi registry-factory'ye taşınır (config.model → provider)
internal/source → ScanMarkdown/ScanCode, Extractor arayüzüne adaptasyon (aynı davranış, tek yol)
```

Layering kuralı korunur: `cmd` → `pipeline` → `indexer`/`embed`/`extractor` → `store`/`source`. `internal/`
hiçbir zaman `cmd/`'e referans vermez (mevcut kural, bozulmaz).

## 2. Mission cards (her biri: tek commit, tek self-review, E2E kanıt zorunlu)

### M4 — Pipeline unification (step 4)
**Kapsam:** `internal/pipeline` paketi: `Pipeline.Sync(ctx, col)` → sync + hemen `pendingEmbed(chunks)` +
`syncVectorIndex`. Stop-hook tek process'e iner (`seek sync` artık embed de çalıştırır, `--embed/--no-embed`
bayrakları). `seek embed` komutu kalır (elle tetikleme/batch için), kod yolu pipeline'a iner.
**Kanıt:** E2E: tek `sync` çağrısı sonrası `search --vec` çalışır (embed beklemeden); iki `store.Open`
yerine bir; hook lock tekyönlü. Test: pipeline unit (mock embed client), hook exec testi.
**Risk:** lock ve TTL davranışı değişir — `hooks.go` lock yardımcıları korunur, sadece çağrı deseni değişir.

### M5 — Retrieval quality layer (step 5)
**Kapsam:** `internal/search`'e `quality.go`: RRF k + rerank skor birleşimi + result dedup +
`content_kind`-aware snippet üretimi. Aggregation/filter zaten T3'te hazır. Bu kart "taste" kartı:
ışık dokunuşu, davranış korunarak skor mantığının tek yerde toplanması.
**Kanıt:** A/B probe: aynı sorgu, eski vs yeni skor sıralaması — fark olmamalı (davranış-koruyan refactor);
rerank aktifken skor birleşim tek fonksiyonda.

### M6 — Source parsing framework (step 6)
**Kapsam:** `source.ScanMarkdown/ScanCode/ScanImages/PDF` → ortak `SourceHandler` arayüzü
(`Scan(pattern) []FileInfo`), parserdef schema motoru bu arayüzün arkasına toplanır. Indexer registry
(M4'te başlayan) burada tamamlanır: her collection type bir handler kaydı.
**Kanıt:** PACKAGE_SYMBOLS_IDENTICAL tarzı sembol-envanter kanıtı + her format için sync E2E probe.
**Not:** T3'te iki yerde elle metadata yazmak zorunda kalmıştık — bu kart o sızıntıyı söner.

### M7 — Provider abstraction (step 7)
**Kapsam:** `embed.Provider` arayüzü (TextEmbedder, ImageEmbedder, Reranker, OCR yetenek arayüzleri);
`newEmbedClient`/`newVLClient` cmd/embed.go'daki branch'ler factory registry'ye iner. Config değişikliği
yok — mevcut config şeması korunur. `DefaultVLEndpoint` fallback'i istisna olarak kalır (AGENTS.md kuralı).
**Kanıt:** mock provider ile E2E (embed akışı provider-bağımsız); config-driven factory tablo testi.

### M8 — Validation manifest (step 8)
**Kapsam:** tüm kartların tamamlanma kanıtları tek belgede: her kart için commit hash + E2E çıktı +
test envanteri. #7 umbrella kapanışı. Bu belge `docs/plans/phase5-manifest.md` olarak commitlenir.

## 3. Sıralama ve bağımlılık

```
M4 (pipeline) ──→ M6 (handler registry tamamlama) ──→ M7 (provider) ──→ M8 (manifest)
              └→ M5 (quality, bağımsız — paralel olabilir)
```

M4 önce: M6'nın registry deseni M4'ün pipeline passthrough'üne oturur. M5 bağımsız — herhangi bir noktada.

## 4. Bilinçli kapsam dışı (kullanıcı kararı bekliyor)

- `seek mcp install` (#32 step 7 — self-review'da not edilmişti)
- macOS notarization + SHA-pinning/Cosign (#29)
- FTS tokenizer değişikliği / rebuild davranışı (AGENTS.md uyarısı)
