🎯 **What:** Tested the exported `SearchHybrid` function in `Engine` which was previously untested.

📊 **Coverage:** Added test cases for basic hybrid search behavior (BM25 + Vector fusion), fallback behavior when vector search fails, boundary checks like limit being zero, and integration with the VL (Vision-Language) client.

✨ **Result:** Improved test coverage for `internal/search` package, ensuring reliability when modifying the main search engine interface, specifically verifying edge cases and degradation paths.
