"use client";

import {
  useCallback,
  useState,
  type ClipboardEvent as ReactClipboardEvent,
  type RefObject,
} from "react";
import { toast } from "sonner";

import type { ImageAssetImportResponse } from "@/lib/api";
import { importUnifiedImageAssets } from "@/store/image-assets";
import type { ImageMode } from "@/store/image-conversations";
import type {
  StoredImage,
  StoredSourceImage,
} from "@/store/image-conversations";

import { buildImageDataUrl, buildSourceImageUrl } from "../view-utils";
import type { WorkspaceForwardImageItem } from "../workspace-forward";

export type EditorTarget = {
  conversationId: string | null;
  image: StoredImage | null;
  imageName: string;
  sourceDataUrl: string;
};

type UseImageSourceInputsOptions = {
  mode: ImageMode;
  selectedConversationId: string | null;
  setMode: (mode: ImageMode) => void;
  focusConversation: (conversationId: string) => void;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  makeId: () => string;
  autoImportUploadedSources: boolean;
};

async function fileToDataUrl(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(new Error(`读取 ${file.name} 失败`));
    reader.readAsDataURL(file);
  });
}

function buildStoredSourceImageFromURL(payload: {
  id: string;
  role: "image" | "mask";
  name: string;
  url: string;
  origin?: StoredSourceImage["origin"] | WorkspaceForwardImageItem["origin"];
}): StoredSourceImage {
  if (payload.url.startsWith("data:")) {
    return {
      id: payload.id,
      role: payload.role,
      name: payload.name,
      dataUrl: payload.url,
      origin: payload.origin,
    };
  }
  return {
    id: payload.id,
    role: payload.role,
    name: payload.name,
    url: payload.url,
    origin: payload.origin,
  };
}

function fileSourcePath(file: File) {
  const withPath = file as File & {
    path?: string;
    webkitRelativePath?: string;
  };
  return withPath.path || withPath.webkitRelativePath || file.name;
}

function summarizeImportFailures(
  failures: NonNullable<ImageAssetImportResponse["failed"]>,
) {
  return failures
    .map((failure) => {
      const name = String(failure.name || "").trim();
      const error = String(failure.error || "").trim();
      if (!name && !error) {
        return "";
      }
      if (!name) {
        return error;
      }
      if (!error) {
        return name;
      }
      return `${name}：${error}`;
    })
    .filter(Boolean);
}

export function summarizeSourceImportResult(result: ImageAssetImportResponse) {
  const importedItems = Array.isArray(result.items)
    ? result.items.filter((item) => item.imageUrl)
    : [];
  const failed = Array.isArray(result.failed) ? result.failed : [];
  const failureDetails = summarizeImportFailures(failed);
  return {
    importedItems,
    failedCount: failed.length,
    failureDetails,
  };
}

export function shouldImportSourceFiles({
  role,
  autoImportUploadedSources,
}: {
  role: "image" | "mask";
  autoImportUploadedSources: boolean;
}) {
  return role === "image" && autoImportUploadedSources;
}

export function useImageSourceInputs({
  mode,
  selectedConversationId,
  setMode,
  focusConversation,
  textareaRef,
  makeId,
  autoImportUploadedSources,
}: UseImageSourceInputsOptions) {
  const [sourceImages, setSourceImages] = useState<StoredSourceImage[]>([]);
  const [editorTarget, setEditorTarget] = useState<EditorTarget | null>(null);

  const appendFiles = useCallback(
    async (files: File[] | FileList | null, role: "image" | "mask") => {
      const normalizedFiles = files ? Array.from(files) : [];
      if (normalizedFiles.length === 0) {
        return;
      }
      const shouldImport = shouldImportSourceFiles({
        role,
        autoImportUploadedSources,
      });
      if (shouldImport) {
        try {
          const result = await importUnifiedImageAssets(normalizedFiles);
          const { importedItems, failedCount, failureDetails } =
            summarizeSourceImportResult(result);
          let importedCount = 0;
          if (importedItems.length > 0) {
            const nextItems: StoredSourceImage[] = importedItems
              .map((item) =>
                buildStoredSourceImageFromURL({
                  id: makeId(),
                  role,
                  name: item.filename || item.title || "gallery-image.png",
                  url: item.imageUrl || "",
                  origin: {
                    type: "gallery",
                    confirmed: true,
                    gallery: {
                      assetId: item.id,
                      conversationId: item.conversationId,
                      turnId: item.turnId,
                      imageId: item.imageId,
                    },
                    assetTitle: item.title,
                    sourceKind: item.sourceKind ?? undefined,
                    fromPage: "/image/workspace",
                  },
                }),
              );
            if (nextItems.length > 0) {
              importedCount = nextItems.length;
              setSourceImages((prev) => [
                ...prev.filter((item) => item.role !== "mask"),
                ...prev.filter((item) => item.role === "mask"),
                ...nextItems,
              ]);
            }
          }
          if (failedCount > 0) {
            const failureText =
              failureDetails.length > 0 ? `：${failureDetails.join("；")}` : "";
            if (importedCount > 0) {
              toast.error(
                `已添加并同步 ${importedCount} 张来源图到图库，另有 ${failedCount} 张导入失败${failureText}`,
              );
              return;
            }
            toast.error(`来源图导入失败 ${failedCount} 张${failureText}`);
            return;
          }
          if (importedCount > 0) {
            toast.success(`已添加并同步 ${importedCount} 张来源图到图库`);
            return;
          }
        } catch (error) {
          toast.error(
            error instanceof Error
              ? `同步到图库失败：${error.message}`
              : "同步到图库失败，已作为本地来源继续添加",
          );
        }
      }
      const nextItems = await Promise.all(
        normalizedFiles.map(async (file) => ({
          id: makeId(),
          role,
          name: file.name,
          dataUrl: await fileToDataUrl(file),
          origin: {
            type: "file" as const,
            confirmed: false,
            filePath: fileSourcePath(file),
          },
        })),
      );
      setSourceImages((prev) => {
        if (role === "mask") {
          return [...prev.filter((item) => item.role !== "mask"), nextItems[0]];
        }
        return [
          ...prev.filter((item) => item.role !== "mask"),
          ...prev.filter((item) => item.role === "mask"),
          ...nextItems,
        ];
      });
    },
    [autoImportUploadedSources, makeId],
  );

  const handlePromptPaste = useCallback(
    (event: ReactClipboardEvent<HTMLTextAreaElement>) => {
      const clipboardImages = Array.from(event.clipboardData.items)
        .filter(
          (item) => item.kind === "file" && item.type.startsWith("image/"),
        )
        .map((item) => item.getAsFile())
        .filter((file): file is File => Boolean(file));

      if (clipboardImages.length === 0) {
        return;
      }

      event.preventDefault();
      void appendFiles(clipboardImages, "image");
      toast.success(
        mode === "generate" ? "已从剪贴板添加参考图" : "已从剪贴板添加源图",
      );
    },
    [appendFiles, mode],
  );

  const removeSourceImage = useCallback((id: string) => {
    setSourceImages((prev) => prev.filter((item) => item.id !== id));
  }, []);

  const seedFromResult = useCallback(
    (conversationId: string, image: StoredImage, nextMode: ImageMode) => {
      const dataUrl = buildImageDataUrl(image);
      if (!dataUrl) {
        toast.error("当前图片没有可复用的数据");
        return;
      }
      focusConversation(conversationId);
      setMode(nextMode);
      setSourceImages([
        buildStoredSourceImageFromURL({
          id: makeId(),
          role: "image",
          name: "source.png",
          url: dataUrl,
        }),
      ]);
      textareaRef.current?.focus();
    },
    [focusConversation, makeId, setMode, textareaRef],
  );

  const openSelectionEditor = useCallback(
    (
      conversationId: string,
      _turnId: string,
      image: StoredImage,
      imageName: string,
    ) => {
      const dataUrl = buildImageDataUrl(image);
      if (!dataUrl) {
        toast.error("当前图片没有可复用的数据");
        return;
      }
      setEditorTarget({
        conversationId,
        image,
        imageName,
        sourceDataUrl: dataUrl,
      });
    },
    [],
  );

  const openSourceSelectionEditor = useCallback(
    (sourceImageId: string) => {
      const sourceImage = sourceImages.find(
        (item) => item.id === sourceImageId && item.role === "image",
      );
      if (!sourceImage) {
        toast.error("当前源图不可用于选区编辑");
        return;
      }
      const sourceURL = buildSourceImageUrl(sourceImage);
      if (!sourceURL) {
        toast.error("当前源图不可用于选区编辑");
        return;
      }

      setEditorTarget({
        conversationId: selectedConversationId,
        image: null,
        imageName: sourceImage.name || "source.png",
        sourceDataUrl: sourceURL,
      });
    },
    [selectedConversationId, sourceImages],
  );

  const closeSelectionEditor = useCallback(() => {
    setEditorTarget(null);
  }, []);

  const appendForwardedImages = useCallback(
    (items: WorkspaceForwardImageItem[]) => {
      if (items.length === 0) {
        return;
      }
      const nextItems = items.map((item) =>
        buildStoredSourceImageFromURL({
          id: makeId(),
          role: item.role,
          name: item.name,
          url: item.url,
          origin: item.origin,
        }),
      );
      setSourceImages((prev) => {
        const nextMask = nextItems.find((item) => item.role === "mask");
        const nextImages = nextItems.filter((item) => item.role === "image");
        const prevImages = prev.filter((item) => item.role === "image");
        const prevMask = prev.find((item) => item.role === "mask");
        return [
          ...prevImages,
          ...nextImages,
          ...(nextMask ? [nextMask] : prevMask ? [prevMask] : []),
        ];
      });
    },
    [makeId],
  );

  return {
    sourceImages,
    setSourceImages,
    editorTarget,
    setEditorTarget,
    appendFiles,
    handlePromptPaste,
    removeSourceImage,
    seedFromResult,
    openSelectionEditor,
    openSourceSelectionEditor,
    closeSelectionEditor,
    appendForwardedImages,
  };
}
