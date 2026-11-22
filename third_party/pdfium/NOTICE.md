# pdfium, as WebAssembly

ferry renders a PDF's first page with PDFium compiled to WebAssembly. The
module is not in this repository: it comes embedded in
[go-pdfium](https://github.com/klippa-app/go-pdfium) (MIT), whose
`internal/pdfium_wasm/pdfium.wasm` ferry links in, and is built by that
project from [PDFium](https://pdfium.googlesource.com/pdfium/) — its build
recipe and the exact PDFium revision are in go-pdfium's repository, at the
version `go.mod` names.

PDFium is under the BSD 3-Clause licence (Copyright 2014 The PDFium
Authors). It bundles libraries under their own permissive licences, among
them FreeType (FreeType License), libjpeg-turbo (IJG and BSD 3-Clause),
OpenJPEG (BSD 2-Clause), Little CMS (MIT), zlib (zlib License) and Abseil
(Apache 2.0); PDFium's `third_party/` holds each one's licence text. None
of them is copyleft.

ferry runs the module in [wazero](https://github.com/tetratelabs/wazero)
(Apache 2.0), without a file system and with a fixed memory limit
(internal/preview).
