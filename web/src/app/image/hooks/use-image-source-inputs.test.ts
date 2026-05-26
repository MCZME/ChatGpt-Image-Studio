import { describe, expect, it } from "vitest";

import {
  shouldImportSourceFiles,
  summarizeSourceImportResult,
} from "./use-image-source-inputs";

describe("use-image-source-inputs helpers", () => {
  it("only imports uploaded source images when auto-import is enabled for image role", () => {
    expect(
      shouldImportSourceFiles({
        role: "image",
        autoImportUploadedSources: true,
      }),
    ).toBe(true);
    expect(
      shouldImportSourceFiles({
        role: "image",
        autoImportUploadedSources: false,
      }),
    ).toBe(false);
    expect(
      shouldImportSourceFiles({
        role: "mask",
        autoImportUploadedSources: true,
      }),
    ).toBe(false);
  });

  it("keeps successful imported items and exposes failed details", () => {
    const result = summarizeSourceImportResult({
      items: [
        {
          id: "asset-1",
          title: "success",
          prompt: "",
          mode: "import",
          model: "external",
          createdAt: "2026-05-26T00:00:00Z",
          imageUrl: "/images/success.png",
          filename: "success.png",
        },
        {
          id: "asset-2",
          title: "missing-url",
          prompt: "",
          mode: "import",
          model: "external",
          createdAt: "2026-05-26T00:00:00Z",
        },
      ],
      failed: [
        { name: "large.png", error: "image exceeds max upload size" },
        { name: "bad.txt", error: "file is not a supported image" },
      ],
    });

    expect(result.importedItems).toHaveLength(1);
    expect(result.importedItems[0]?.filename).toBe("success.png");
    expect(result.failedCount).toBe(2);
    expect(result.failureDetails).toEqual([
      "large.png：image exceeds max upload size",
      "bad.txt：file is not a supported image",
    ]);
  });
});
