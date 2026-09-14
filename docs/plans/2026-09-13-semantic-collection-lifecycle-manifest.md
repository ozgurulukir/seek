# Semantic Coverage & Collection Lifecycle — Uygulama Manifesti

**Tarih:** 2026-09-13<br>
**Plan:** `docs/plans/2026-09-13-semantic-collection-lifecycle-plan.md` (commit edilmez)<br>
**Durum:** tamamlandı

Bu belge, semantic-collection-lifecycle planının (S1–S6 kartları) uygulama ve
doğrulama kanıtlarını tek yerde toplar. Plan dosyasının kendisi
("bu belge commit edilmeyecek") commit dışı bırakılmıştır.

## Kart kanıtları

| Kart | Kapsam | Uygulama yeri |
| --- | --- | --- |
| S1 | Ortak `DocumentEnricher` seam + native/semantic fast-field merge | `internal/indexer/enrich.go` (`semanticEligible`, `mergeFastFields`) |
| S2 | Document-scoped semantic fingerprint/status persistence; hazır/batch stale seçimi; hata durumunda alan koruma | `internal/store/semantic.go` (`SemanticFingerprint`, `SemanticStatus`, `UpdateSemanticState`, `GetStaleSemanticDocuments`), `internal/store/fastfield.go` |
| S3 | Collection-scoped semantic backfill; processed/skipped/failed raporu; degrade-on-failure | `internal/indexer/backfill.go` (`BackfillSemantic`), `internal/app/collection.go` (`Backfill`) |
| S4 | Collection lifecycle service (list/show/rename/reindex/sync-path); embedding profile contract'ı | `internal/app/collection.go`, `internal/store` (atomic rename, profile validation) |
| S5 | `seek collection list|show|rename|reindex [--semantic-only]`; eski `status`/`rm` alias; MCP salt-okuma | `cmd/collection.go`, `cmd/sync.go`, `cmd/status.go`, `cmd/rm.go`, `main.go`; `cmd/mcp.go` (yazma aracı eklenmedi) |
| S6 | Dokümantasyon: tek tutarlı semantic davranış, backfill, servis-down, kaynak dosya güvencesi, komut yüzeyi | `AGENTS.md`, `README.md`, `docs/semantic.md`, `docs/query-guide.md`, `docs/mcp.md`, `skills/seek/SKILL.md`, `skills/seek/references/collection-types.md`, `skills/seek/references/filters.md` |

## Davranış özeti (belgelendiği haliyle)

- Semantic enrichment iki katmanlı çalışır; per-format destek matrisi yoktur:
  - Conversations/PDF/documents `seek sync` sırasında zenginleşir.
  - Markdown/code/parser sync sırasında zenginleşmez; `seek collection
    reindex <name> --semantic-only` backfill'i ile kapsanır.
- Servis kapalıysa sync başarılı olur (WARN), mevcut semantic fast field'lar
  silinmez, durum `stale`/`error` olur; sonraki backfill yeniden dener.
- Backfill yalnız indekslenmiş chunk'lardan yeniden zenginleştirir; kaynak
  dosya, FTS, embedding ve vector index'e dokunmaz. Rapor formatı:
  `N processed, M skipped, K failed`.
- Hiçbir yönetim komutu kaynak dosyaları değiştirmez/silmez/taşımaz; kaynak
  dosyalar gerçeklik kaynağıdır.
- Tam reindex embedding profile sözleşmesine uyar: vektör uzayı uyuşmazsa
  reddeder (reindex ipucu ile), `--allow-vector-space-change` ile
  clear/re-embed seçilebilir.

## Doğrulama kanıtları

- `cmd/collection_test.go` — list/show/rename/reindex sözleşmesi, profile
  mismatch reddi, `--semantic-only` raporu (processed/skipped/failed).
- `cmd/sync_path_test.go` — `sync <col> --path` CLI yüzeyi: dış path reddi ve
  path'in scope filtresi olmadığının (whole-collection sync) kanıtı.
- `internal/app/collection_test.go` — CollectionService list/show/sync-path/
  backfill; `ValidateCollectionPath` güvenlik doğrulaması (dış path,
  symlink/junction escape, Windows case-normalize reddi).
- `internal/indexer/backfill_test.go` — E2E markdown backfill, degrade-on-
  failure, başarısızlık sonrası yeniden deneme, stale alanların değişimi,
  izolasyon (`--semantic-only` sonrası source/FTS/embedding değişmez),
  cancellation.
- `internal/indexer/enrich_test.go` — `semanticEligible` matrisi, merge
  politikası.
- `internal/store/semantic_store_test.go` — stale seçim, atomic güncelleme,
  hata durumunda alan koruma, fingerprint kararlılığı.

## Not

S1 seam'i sync sırasında yalnızca conversations/PDF/documents'ı
zenginleştirir; markdown/code semantic kapsamı backfill üzerinden sağlanır.
`internal/indexer/enrich.go` üst yorumundaki "later cards" ifadesi bu
kapsamı artık eksik anlatır; davranış değişikliği yapılmaksızın yalnızca
yorum güncellenebilir (kod dışı, dokümantasyon notu).