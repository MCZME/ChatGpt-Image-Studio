"use client";

import {
  deleteImageAsset as deleteServerImageAsset,
  deleteImageAssetsBulk as deleteServerImageAssetsBulk,
  fetchConfig,
  getImageAsset as getServerImageAsset,
  importImageAssets as importServerImageAssets,
  listImageAssets as listServerImageAssets,
  syncImageAssets,
  updateImageAsset as updateServerImageAsset,
  updateImageAssetsBulk as updateServerImageAssetsBulk,
  type ImageAsset,
  type ImageAssetListResponse,
  type ConfigPayload,
} from "@/lib/api";
import {
  listImageConversations,
  type ImageConversation,
  type ImageConversationTurn,
  type StoredImage,
} from "@/store/image-conversations";
import { buildAssetSourceImages } from "@/store/image-asset-source-images";

function normalizeText(value?: string) {
  return String(value || "").trim();
}

function summarizePrompt(prompt: string) {
  const cleaned = normalizeText(prompt);
  if (!cleaned) {
    return "未命名图片";
  }
  return cleaned.length <= 32 ? cleaned : `${cleaned.slice(0, 32)}...`;
}

function normalizeTags(tags?: string[]) {
  if (!Array.isArray(tags) || tags.length === 0) {
    return undefined;
  }
  const normalized = Array.from(
    new Set(tags.map((tag) => normalizeText(tag)).filter(Boolean)),
  );
  return normalized.length > 0 ? normalized : undefined;
}

function buildAssetId(
  conversation: ImageConversation,
  turn: ImageConversationTurn,
  image: StoredImage,
  index: number,
) {
  const imageId = normalizeText(image.id) || `image-${index}`;
  return [conversation.id, turn.id, imageId]
    .map((item) => item.replace(/[\\/]/g, "-"))
    .join("::");
}

function buildAssetFromTurn(
  conversation: ImageConversation,
  turn: ImageConversationTurn,
  image: StoredImage,
  index: number,
): ImageAsset | null {
  if (!normalizeText(image.url) && !normalizeText(image.b64_json)) {
    return null;
  }
  return {
    id: buildAssetId(conversation, turn, image, index),
    title: normalizeText(turn.title) || summarizePrompt(turn.prompt),
    prompt: normalizeText(turn.prompt),
    revisedPrompt: normalizeText(image.revised_prompt) || undefined,
    category: normalizeText(turn.category) || undefined,
    tags: normalizeTags(turn.tags),
    mode: turn.mode,
    model: normalizeText(turn.model),
    createdAt: normalizeText(turn.createdAt),
    conversationId: normalizeText(conversation.id) || undefined,
    turnId: normalizeText(turn.id) || undefined,
    imageId: normalizeText(image.id) || undefined,
    status: normalizeText(image.status) || undefined,
    imageUrl: normalizeText(image.url) || undefined,
    fileId: normalizeText(image.file_id) || undefined,
    genId: normalizeText(image.gen_id) || undefined,
    sourceAccountId: normalizeText(image.source_account_id) || undefined,
    sourceImages: buildAssetSourceImages(turn.sourceImages),
  };
}

function collectConversationAssets(conversations: ImageConversation[]) {
  const items: ImageAsset[] = [];
  for (const conversation of conversations) {
    const turns =
      Array.isArray(conversation.turns) && conversation.turns.length > 0
        ? conversation.turns
        : [];
    for (const turn of turns) {
      for (const [index, image] of (turn.images || []).entries()) {
        const item = buildAssetFromTurn(conversation, turn, image, index);
        if (item) {
          items.push(item);
        }
      }
    }
  }
  return items;
}

async function syncConversationAssetsToBackend() {
  const conversations = await listImageConversations();
  const assets = collectConversationAssets(conversations);
  await syncImageAssets(assets);
}

async function ensureAssetSyncConfig() {
  try {
    return await fetchConfig();
  } catch {
    return null;
  }
}

export async function listUnifiedImageAssets(params?: {
  query?: string;
  category?: string;
  tag?: string;
  favorite?: boolean;
  limit?: number;
  offset?: number;
  sort?: string;
}): Promise<ImageAssetListResponse> {
  const config: ConfigPayload | null = await ensureAssetSyncConfig();
  if (!config || config.storage.imageConversationStorage !== "server") {
    await syncConversationAssetsToBackend();
  }
  return listServerImageAssets(params);
}

export async function syncUnifiedImageAssets() {
  await syncConversationAssetsToBackend();
}

export async function getUnifiedImageAsset(id: string) {
  const config: ConfigPayload | null = await ensureAssetSyncConfig();
  if (!config || config.storage.imageConversationStorage !== "server") {
    await syncConversationAssetsToBackend();
  }
  return getServerImageAsset(id);
}

export async function importUnifiedImageAssets(
  files: File[],
  options: {
    category?: string;
    tags?: string[];
    note?: string;
  } = {},
) {
  return importServerImageAssets(files, options);
}

export async function updateUnifiedImageAsset(
  id: string,
  payload: {
    title?: string;
    category?: string;
    tags?: string[];
    note?: string;
    favorite?: boolean;
  },
) {
  return updateServerImageAsset(id, payload);
}

export async function updateUnifiedImageAssetsBulk(payload: {
  ids: string[];
  title?: string;
  category?: string;
  tags?: string[];
  note?: string;
  favorite?: boolean;
}) {
  return updateServerImageAssetsBulk(payload);
}

export async function deleteUnifiedImageAsset(
  id: string,
  options: {
    deleteFile?: boolean;
  } = {},
) {
  return deleteServerImageAsset(id, options);
}

export async function deleteUnifiedImageAssetsBulk(payload: {
  ids: string[];
  deleteFile?: boolean;
}) {
  return deleteServerImageAssetsBulk(payload);
}
