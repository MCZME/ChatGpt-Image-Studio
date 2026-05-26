import { describe, expect, it } from "vitest";

import { buildAssetSourceImages } from "./image-asset-source-images";

describe("image-asset-source-images", () => {
  it("builds lightweight asset source references without data urls", () => {
    const items = buildAssetSourceImages([
      {
        id: "gallery-source",
        role: "image",
        name: "gallery.png",
        url: "/v1/files/image/gallery.png",
        dataUrl: "data:image/png;base64,AAAA",
        origin: {
          type: "gallery",
          confirmed: true,
          gallery: {
            assetId: "asset-1",
            conversationId: "conv-1",
            turnId: "turn-1",
            imageId: "img-1",
          },
        },
      },
      {
        id: "local-source",
        role: "image",
        name: "local.png",
        dataUrl: "data:image/png;base64,BBBB",
        origin: {
          type: "file",
          confirmed: false,
          filePath: "C:\\tmp\\local.png",
        },
      },
    ]);

    expect(items).toEqual([
      {
        id: "gallery-source",
        role: "image",
        name: "gallery.png",
        url: "/v1/files/image/gallery.png",
        origin: {
          type: "gallery",
          confirmed: true,
          gallery: {
            assetId: "asset-1",
            conversationId: "conv-1",
            turnId: "turn-1",
            imageId: "img-1",
          },
        },
        source: {
          type: "gallery",
          confirmed: true,
          gallery: {
            assetId: "asset-1",
            conversationId: "conv-1",
            turnId: "turn-1",
            imageId: "img-1",
          },
        },
      },
      {
        id: "local-source",
        role: "image",
        name: "local.png",
        origin: {
          type: "file",
          confirmed: false,
          filePath: "C:\\tmp\\local.png",
        },
        source: {
          type: "file",
          confirmed: false,
          filePath: "C:\\tmp\\local.png",
        },
      },
    ]);
    expect(JSON.stringify(items)).not.toContain("data:image");
    expect(JSON.stringify(items)).not.toContain("AAAA");
    expect(JSON.stringify(items)).not.toContain("BBBB");
  });
});
