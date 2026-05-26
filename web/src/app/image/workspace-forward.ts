"use client";

import type { ImageMode } from "@/store/image-conversations";

export const WORKSPACE_FORWARD_QUERY_KEY = "wf";
const WORKSPACE_FORWARD_VERSION = 1;

export type WorkspaceForwardImageOrigin = {
  type: "gallery";
  confirmed?: boolean;
  gallery: {
    assetId: string;
    index?: number;
    conversationId?: string;
    turnId?: string;
    imageId?: string;
  };
  assetTitle?: string;
  sourceKind?: string;
  fromPage?: string;
};

export type WorkspaceForwardImageItem = {
  role: "image" | "mask";
  name: string;
  url: string;
  origin?: WorkspaceForwardImageOrigin;
};

export type WorkspaceForwardPayload = {
  version: number;
  title?: string;
  mode?: ImageMode;
  prompt?: string;
  count?: number;
  category?: string;
  tags?: string[];
  images?: WorkspaceForwardImageItem[];
};

function normalizeText(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function normalizeTags(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return Array.from(
    new Set(
      value
        .map((item) => normalizeText(item))
        .filter(Boolean),
    ),
  );
}

function normalizeImageItem(value: unknown): WorkspaceForwardImageItem | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const item = value as Record<string, unknown>;
  const role = item.role === "mask" ? "mask" : "image";
  const name = normalizeText(item.name) || (role === "mask" ? "mask.png" : "image.png");
  const url = normalizeText(item.url);
  if (!url) {
    return null;
  }

  const rawOrigin =
    item.origin && typeof item.origin === "object"
      ? (item.origin as Record<string, unknown>)
      : null;
  const rawGallery =
    rawOrigin?.gallery && typeof rawOrigin.gallery === "object"
      ? (rawOrigin.gallery as Record<string, unknown>)
      : null;
  const assetId =
    normalizeText(rawGallery?.assetId) || normalizeText(rawOrigin?.assetId);
  const index =
    typeof rawGallery?.index === "number" && Number.isFinite(rawGallery.index)
      ? Math.max(0, Math.floor(rawGallery.index))
      : undefined;
  const origin =
    (rawOrigin?.type === "gallery" || rawOrigin?.type === "gallery-asset") && assetId
      ? {
          type: "gallery" as const,
          confirmed:
            typeof rawOrigin?.confirmed === "boolean"
              ? rawOrigin.confirmed
              : true,
          gallery: {
            assetId,
            index,
            conversationId: normalizeText(rawGallery?.conversationId) || undefined,
            turnId: normalizeText(rawGallery?.turnId) || undefined,
            imageId: normalizeText(rawGallery?.imageId) || undefined,
          },
          assetTitle: normalizeText(rawOrigin.assetTitle) || undefined,
          sourceKind: normalizeText(rawOrigin.sourceKind) || undefined,
          fromPage: normalizeText(rawOrigin.fromPage) || undefined,
        }
      : undefined;

  return {
    role,
    name,
    url,
    origin,
  };
}

export function parseWorkspaceForwardPayload(search: string) {
  const params = new URLSearchParams(search);
  const raw = params.get(WORKSPACE_FORWARD_QUERY_KEY);
  if (!raw) {
    return null;
  }

  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    if (parsed.version !== WORKSPACE_FORWARD_VERSION) {
      return null;
    }

    const mode =
      parsed.mode === "edit" || parsed.mode === "generate"
        ? parsed.mode
        : undefined;
    const title = normalizeText(parsed.title) || undefined;
    const prompt = normalizeText(parsed.prompt) || undefined;
    const category = normalizeText(parsed.category) || undefined;
    const tags = normalizeTags(parsed.tags);
    const images = Array.isArray(parsed.images)
      ? parsed.images
          .map(normalizeImageItem)
          .filter((item): item is WorkspaceForwardImageItem => Boolean(item))
      : [];
    const count =
      typeof parsed.count === "number" && Number.isFinite(parsed.count)
        ? Math.max(1, Math.min(8, Math.floor(parsed.count)))
        : undefined;

    return {
      version: WORKSPACE_FORWARD_VERSION,
      title,
      mode,
      prompt,
      count,
      category,
      tags,
      images,
    } satisfies WorkspaceForwardPayload;
  } catch {
    return null;
  }
}

export function buildWorkspaceForwardSearch(
  payload: Omit<WorkspaceForwardPayload, "version">,
) {
  const normalized: WorkspaceForwardPayload = {
    version: WORKSPACE_FORWARD_VERSION,
    title: normalizeText(payload.title) || undefined,
    mode: payload.mode,
    prompt: normalizeText(payload.prompt) || undefined,
    count:
      typeof payload.count === "number" && Number.isFinite(payload.count)
        ? Math.max(1, Math.min(8, Math.floor(payload.count)))
        : undefined,
    category: normalizeText(payload.category) || undefined,
    tags: normalizeTags(payload.tags),
    images: (payload.images ?? [])
      .map(normalizeImageItem)
      .filter((item): item is WorkspaceForwardImageItem => Boolean(item)),
  };

  const params = new URLSearchParams();
  params.set(WORKSPACE_FORWARD_QUERY_KEY, JSON.stringify(normalized));
  return params.toString();
}
