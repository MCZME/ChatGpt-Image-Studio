"use client";

import type {
  ImageAsset,
  ImageAssetSourceImage,
  ImageSourceOrigin,
} from "@/lib/api";
import type { StoredSourceImage } from "@/store/image-conversations";

function normalizeText(value?: string) {
  return String(value || "").trim();
}

function normalizeTags(tags?: string[]) {
  if (!Array.isArray(tags) || tags.length === 0) {
    return undefined;
  }
  const normalized = Array.from(
    new Set(tags.map((item) => normalizeText(item)).filter(Boolean)),
  );
  return normalized.length > 0 ? normalized : undefined;
}

function normalizeOrigin(
  origin: ImageSourceOrigin | undefined,
  fallbackUrl?: string,
): ImageSourceOrigin | undefined {
  if (!origin) {
    const url = normalizeText(fallbackUrl);
    return url
      ? {
          type: "url",
          confirmed: true,
          url,
        }
      : undefined;
  }
  const gallery = origin.gallery
    ? {
        assetId: normalizeText(origin.gallery.assetId) || undefined,
        index:
          typeof origin.gallery.index === "number"
            ? origin.gallery.index
            : undefined,
        conversationId:
          normalizeText(origin.gallery.conversationId) || undefined,
        turnId: normalizeText(origin.gallery.turnId) || undefined,
        imageId: normalizeText(origin.gallery.imageId) || undefined,
      }
    : undefined;
  const next: ImageSourceOrigin = {
    type: normalizeText(origin.type) || undefined,
    confirmed: Boolean(origin.confirmed),
    url: normalizeText(origin.url) || undefined,
    filePath: normalizeText(origin.filePath) || undefined,
    gallery:
      gallery &&
      (gallery.assetId ||
        gallery.conversationId ||
        gallery.turnId ||
        gallery.imageId ||
        typeof gallery.index === "number")
        ? gallery
        : undefined,
  };
  if (!next.type) {
    if (next.gallery) {
      next.type = "gallery";
    } else if (next.filePath) {
      next.type = "file";
    } else if (next.url) {
      next.type = "url";
    }
  }
  if (!next.url) {
    const url = normalizeText(fallbackUrl);
    if (url && next.type === "url") {
      next.url = url;
      next.confirmed = next.confirmed ?? true;
    }
  }
  if (!next.type && !next.url && !next.filePath && !next.gallery) {
    return undefined;
  }
  return next;
}

export function buildAssetSourceImages(
  items: StoredSourceImage[] | undefined,
): ImageAssetSourceImage[] | undefined {
  if (!Array.isArray(items) || items.length === 0) {
    return undefined;
  }
  const result = items
    .map((item) => {
      const url = normalizeText(item.url);
      const origin = normalizeOrigin(item.origin, url);
      const next: ImageAssetSourceImage = {
        id: normalizeText(item.id) || undefined,
        role: normalizeText(item.role) || undefined,
        name: normalizeText(item.name) || undefined,
        url: url || undefined,
        origin,
        source: origin,
      };
      return next.id || next.role || next.name || next.url || next.origin
        ? next
        : null;
    })
    .filter((item): item is ImageAssetSourceImage => Boolean(item));
  return result.length > 0 ? result : undefined;
}

export function summarizeAssetSourceImage(source: ImageAssetSourceImage) {
  const origin = source.origin ?? source.source;
  const role = normalizeText(source.role) || "image";
  const name = normalizeText(source.name);
  if (origin?.gallery?.assetId) {
    return {
      role,
      label: name || origin.gallery.assetId,
      detail: `图库引用 · ${origin.gallery.assetId}`,
    };
  }
  if (origin?.type === "file" && origin.filePath) {
    return {
      role,
      label: name || "本地文件",
      detail: `本地文件 · ${origin.filePath}`,
    };
  }
  if (origin?.type === "url" && (origin.url || source.url)) {
    const url = origin.url || source.url || "";
    return {
      role,
      label: name || url,
      detail: `远程链接 · ${url}`,
    };
  }
  if (source.url) {
    return {
      role,
      label: name || source.url,
      detail: `图片地址 · ${source.url}`,
    };
  }
  return {
    role,
    label: name || "未命名参考图",
    detail: origin?.type ? `来源 · ${origin.type}` : "来源信息未记录",
  };
}

export function withAssetSourceImages(
  asset: ImageAsset,
  sourceImages: StoredSourceImage[] | undefined,
): ImageAsset {
  return {
    ...asset,
    sourceImages: buildAssetSourceImages(sourceImages),
    tags: normalizeTags(asset.tags),
  };
}
