# /// script
# dependencies = [
#   "fastapi",
#   "httpx",
#   "uvicorn",
#   "python-multipart",
#   "pypdf",
#   "python-docx",
#   "openpyxl",
#   "beautifulsoup4",
# ]
# ///

from fastapi import FastAPI, UploadFile, File, Form, HTTPException
from typing import List, Optional
import uvicorn
from bs4 import BeautifulSoup
import docx
import pypdf
import openpyxl

# Bounded upload reading lives in a stdlib-only module so it can be tested
# without the parsing dependencies. MAX_UPLOAD_BYTES defaults to 100 MiB and
# can be overridden with XBERG_MAX_UPLOAD_BYTES (invalid values fall back to
# the default).
from upload_limit import MAX_UPLOAD_BYTES, UploadTooLargeError, read_upload_limited

app = FastAPI(title="xberg serve compatible API")

SUPPORTED_FORMATS = [
    {"extension": ".docx", "mime_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
    {"extension": ".xlsx", "mime_type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
    {"extension": ".pdf", "mime_type": "application/pdf"},
    {"extension": ".html", "mime_type": "text/html"},
    {"extension": ".txt", "mime_type": "text/plain"},
    {"extension": ".csv", "mime_type": "text/csv"},
]

@app.get("/health")
def health():
    return {"status": "ok"}

@app.get("/formats")
def formats():
    return SUPPORTED_FORMATS

@app.post("/extract")
async def extract(file: UploadFile = File(...), output_format: Optional[str] = Form("markdown")):
    filename = file.filename or "unknown.txt"
    ext = "." + filename.rsplit(".", 1)[-1].lower() if "." in filename else ""

    # Content-Length is advisory: reject an obviously oversized upload early,
    # but rely on read_upload_limited below to count the bytes actually read.
    if file.size is not None and file.size > MAX_UPLOAD_BYTES:
        raise HTTPException(status_code=413, detail=f"Uploaded file exceeds {MAX_UPLOAD_BYTES} byte limit")

    try:
        spooled = await read_upload_limited(file, MAX_UPLOAD_BYTES)
    except UploadTooLargeError as exc:
        raise HTTPException(status_code=413, detail=str(exc)) from exc

    extracted_text = ""
    mime_type = "text/plain"

    try:
        spooled.seek(0)
        if ext == ".docx":
            mime_type = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
            doc = docx.Document(spooled)
            extracted_text = "\n\n".join([p.text for p in doc.paragraphs if p.text.strip()])
        elif ext == ".xlsx":
            mime_type = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            wb = openpyxl.load_workbook(spooled, data_only=True)
            lines = []
            for sheet in wb.sheetnames:
                ws = wb[sheet]
                lines.append(f"## Sheet: {sheet}")
                for row in ws.iter_rows(values_only=True):
                    row_vals = [str(v) if v is not None else "" for v in row]
                    if any(row_vals):
                        lines.append(" | ".join(row_vals))
            extracted_text = "\n".join(lines)
        elif ext == ".pdf":
            mime_type = "application/pdf"
            reader = pypdf.PdfReader(spooled)
            extracted_text = "\n\n".join([p.extract_text() for p in reader.pages if p.extract_text()])
        elif ext in [".html", ".htm"]:
            mime_type = "text/html"
            soup = BeautifulSoup(spooled, "html.parser")
            extracted_text = soup.get_text(separator="\n\n")
        else:
            extracted_text = spooled.read().decode("utf-8", errors="replace")
            mime_type = "text/plain"

        if not extracted_text.strip():
            extracted_text = f"Content extracted from {filename}"

        return {
            "results": [
                {
                    "content": extracted_text,
                    "mime_type": mime_type,
                }
            ],
            "errors": [],
            "summary": {
                "inputs": 1,
                "results": 1,
                "errors": 0
            }
        }
    except Exception as e:
        # Never expose raw exception text to API callers; it can leak
        # filesystem paths and library internals via stack-trace details.
        # Only the exception class name is returned; the details go to logs.
        print(f"xberg_server: extraction failed for {filename}: {e!r}")
        return {
            "results": [],
            "errors": [{"input": filename,
                        "message": f"extraction failed ({type(e).__name__})"}],
            "summary": {
                "inputs": 1,
                "results": 0,
                "errors": 1
            }
        }
    finally:
        spooled.close()

if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=8001)
