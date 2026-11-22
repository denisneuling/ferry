# libheif and libde265, as WebAssembly

ferry makes thumbnails of HEIC photos with libheif and libde265, compiled
to one WebAssembly module, `heif.wasm.gz`, which ferry embeds and runs in
wazero — without a file system, with a fixed memory limit, a new instance
for every photo (internal/preview).

| | |
|---|---|
| libheif | 1.18.2, <https://github.com/strukturag/libheif> — GNU LGPL 3.0 or later |
| libde265 | 1.0.15, <https://github.com/strukturag/libde265> — GNU LGPL 3.0 or later |
| the glue (`build/heif.c`) and the recipe (`build/Makefile`) | from gen2brain/heic v0.4.9, <https://github.com/gen2brain/heic> — MIT (`LICENSE.gen2brain`) |
| `heif.wasm` (unpacked) | sha256 `04e7804a4ef20096660ac9347face433661b3c06e4da825c2dac15c06f101657` |

The licence texts: `COPYING.LESSER` (LGPL 3.0), `COPYING` (GPL 3.0, which
the LGPL builds on), `LICENSE.libheif`, `LICENSE.libde265` (the projects'
own notes) and `LICENSE.gen2brain`. ferry itself is under the Apache License
2.0; the module is a separate work it runs.

## Building the module

`build/Makefile` is the recipe the module was made with: it checks out
libheif and libde265 at the versions above, applies its changes (`sed`
lines: no file access, no threads), and builds both with the WASI SDK
(`WASI_SDK_PATH`, default `/opt/wasi-sdk`) together with `build/heif.c`,
which exports `malloc`, `free` and `decode`:

    cd third_party/heif/build && make
    gzip -9 -c heif.wasm > ../heif.wasm.gz

## Running ferry with another module

`FERRY_HEIF_WASM=/path/to/heif.wasm` makes ferry load that module instead
of the one it carries — one built from changed sources, for instance. It
must export the same three functions. ferry compiles it when it is first
needed (charged to `FERRY_THUMBNAIL_MEMORY`, about 80 MB at its peak) as the
compiled code in the image is for the module ferry carries.

Each release carries the module as `ferry-heif.wasm`, with this file and the
licence texts as `ferry-heif-LICENSES.md`, both in `SHA256SUMS`.
