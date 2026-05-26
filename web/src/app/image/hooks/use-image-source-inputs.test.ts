import { describe, expect, it } from "vitest";

import { shouldImportSourceFiles } from "./use-image-source-inputs";

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
});
