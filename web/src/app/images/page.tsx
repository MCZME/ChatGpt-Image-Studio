"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CheckSquare,
  Copy,
  Database,
  Heart,
  LoaderCircle,
  Trash2,
  Upload,
  Search,
  Square,
  Tag,
  Tags,
  TextCursorInput,
} from "lucide-react";
import { toast } from "sonner";

import { AppImage } from "@/components/app-image";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { fetchImageAssetStats, type ImageAsset } from "@/lib/api";
import {
  deleteUnifiedImageAsset,
  deleteUnifiedImageAssetsBulk,
  listUnifiedImageAssets,
  importUnifiedImageAssets,
  updateUnifiedImageAsset,
  updateUnifiedImageAssetsBulk,
} from "@/store/image-assets";

function normalizeImageURL(raw?: string) {
  const trimmed = String(raw || "").trim();
  if (!trimmed) {
    return "";
  }
  if (/^(data:|https?:\/\/)/i.test(trimmed)) {
    return trimmed;
  }
  return trimmed;
}

function formatAssetTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function formatAssetSize(value?: number | null) {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    return "";
  }
  const units = ["B", "KB", "MB", "GB"] as const;
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const digits = unitIndex === 0 || size >= 10 ? 0 : 1;
  return `${size.toFixed(digits)} ${units[unitIndex]}`;
}

function formatAssetKind(value?: string | null) {
  const cleaned = String(value || "").trim();
  if (!cleaned) {
    return "";
  }
  const known: Record<string, string> = {
    local: "本地文件",
    remote: "远程链接",
    legacy: "旧版数据",
    generated: "生成图片",
    uploaded: "上传图片",
    conversation: "会话同步",
  };
  return known[cleaned] || cleaned.replace(/[_-]+/g, " ");
}

function formatAssetHash(value?: string | null) {
  const cleaned = String(value || "").trim();
  if (!cleaned) {
    return "";
  }
  return cleaned.length > 24 ? `${cleaned.slice(0, 12)}...${cleaned.slice(-8)}` : cleaned;
}

async function copyText(value: string, successMessage: string) {
  const cleaned = String(value || "").trim();
  if (!cleaned) {
    toast.warning("没有可复制的内容");
    return;
  }
  try {
    await navigator.clipboard.writeText(cleaned);
    toast.success(successMessage);
  } catch {
    toast.error("复制失败");
  }
}

function splitTagsInput(value: string) {
  return Array.from(
    new Set(
      value
        .split(/[,\n，]/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  );
}

function countImportErrors(
  result: Awaited<ReturnType<typeof importUnifiedImageAssets>>,
) {
  const failed =
    Array.isArray(result.failed)
      ? result.failed.length
      : result.errors?.length ?? 0;
  const imported = result.imported ?? result.succeeded ?? 0;
  const skipped = result.skipped ?? result.duplicates ?? 0;
  return { failed, imported, skipped };
}

const IMAGE_PAGE_SIZE = 48;

const SORT_OPTIONS = [
  { value: "created_desc", label: "最新创建" },
  { value: "created_asc", label: "最早创建" },
  { value: "updated_desc", label: "最近更新" },
  { value: "title_asc", label: "标题 A-Z" },
  { value: "title_desc", label: "标题 Z-A" },
  { value: "favorite", label: "收藏优先" },
] as const;

type AssetStorageDetail = {
  label: string;
  value: string;
  title?: string;
  url?: string;
};

type DeleteDialogState =
  | {
      mode: "single";
      asset: ImageAsset;
    }
  | {
      mode: "bulk";
      ids: string[];
      count: number;
    };

function mergeAssetsById(current: ImageAsset[], incoming: ImageAsset[]) {
  const map = new Map(current.map((item) => [item.id, item] as const));
  for (const item of incoming) {
    map.set(item.id, item);
  }
  return Array.from(map.values());
}

export default function ImagesPage() {
  const [items, setItems] = useState<ImageAsset[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [allTags, setAllTags] = useState<string[]>([]);
  const [selectedCategory, setSelectedCategory] = useState("");
  const [selectedTag, setSelectedTag] = useState("");
  const [query, setQuery] = useState("");
  const [selectedSort, setSelectedSort] = useState("created_desc");
  const [favoriteOnly, setFavoriteOnly] = useState(false);
  const [selectedAsset, setSelectedAsset] = useState<ImageAsset | null>(null);
  const [editingCategory, setEditingCategory] = useState("");
  const [editingTags, setEditingTags] = useState("");
  const [editingNote, setEditingNote] = useState("");
  const [selectedAssetIds, setSelectedAssetIds] = useState<string[]>([]);
  const [bulkCategory, setBulkCategory] = useState("");
  const [bulkTags, setBulkTags] = useState("");
  const [tagStats, setTagStats] = useState<Array<{ tag: string; count: number }>>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isBulkSaving, setIsBulkSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isImporting, setIsImporting] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [nextOffset, setNextOffset] = useState(0);
  const [total, setTotal] = useState(0);
  const [deleteDialogState, setDeleteDialogState] =
    useState<DeleteDialogState | null>(null);
  const [deleteFileToo, setDeleteFileToo] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const nextOffsetRef = useRef(0);
  const requestSequenceRef = useRef(0);
  const [importCategory, setImportCategory] = useState("");
  const [importTags, setImportTags] = useState("");
  const [importNote, setImportNote] = useState("");

  const refreshAssets = useCallback(async (mode: "reset" | "append" = "reset") => {
    const requestId = requestSequenceRef.current + 1;
    requestSequenceRef.current = requestId;
    if (mode === "reset") {
      setIsLoading(true);
    } else {
      setIsLoadingMore(true);
    }
    try {
      const [result, stats] = await Promise.all([
        listUnifiedImageAssets({
          query,
          category: selectedCategory || undefined,
          tag: selectedTag || undefined,
          favorite: favoriteOnly,
          limit: IMAGE_PAGE_SIZE,
          offset: mode === "append" ? nextOffsetRef.current : 0,
          sort: selectedSort,
        }),
        fetchImageAssetStats(),
      ]);
      if (requestSequenceRef.current !== requestId) {
        return;
      }
      setItems((current) =>
        mode === "append" ? mergeAssetsById(current, result.items) : result.items,
      );
      setCategories(stats.categories.map((item) => item.category));
      setAllTags(stats.tags.map((item) => item.tag));
      setTagStats(stats.tags);
      setHasMore(result.hasMore);
      setNextOffset(result.nextOffset);
      setTotal(result.total);
    } catch (error) {
      if (requestSequenceRef.current !== requestId) {
        return;
      }
      toast.error(error instanceof Error ? error.message : "读取图片库失败");
    }
    if (requestSequenceRef.current !== requestId) {
      return;
    }
    try {
      if (mode === "reset") {
        setIsLoading(false);
      } else {
        setIsLoadingMore(false);
      }
    } catch {
      // no-op
    }
  }, [favoriteOnly, query, selectedCategory, selectedSort, selectedTag]);

  useEffect(() => {
    void refreshAssets("reset");
  }, [refreshAssets]);

  useEffect(() => {
    nextOffsetRef.current = nextOffset;
  }, [nextOffset]);

  useEffect(() => {
    if (!selectedAsset) {
      setEditingCategory("");
      setEditingTags("");
      setEditingNote("");
      return;
    }
    setEditingCategory(selectedAsset.category || "");
    setEditingTags((selectedAsset.tags || []).join(", "));
    setEditingNote(selectedAsset.note || "");
  }, [selectedAsset]);

  const selectedImageURL = useMemo(
    () => normalizeImageURL(selectedAsset?.imageUrl),
    [selectedAsset?.imageUrl],
  );
  const selectedAssetStorageDetails = useMemo<AssetStorageDetail[]>(() => {
    if (!selectedAsset) {
      return [];
    }
    const originalUrl = String(selectedAsset.originalUrl || "").trim();
    return [
      { label: "文件名", value: String(selectedAsset.filename || "").trim() },
      { label: "大小", value: formatAssetSize(selectedAsset.sizeBytes) },
      { label: "MIME", value: String(selectedAsset.mimeType || "").trim() },
      { label: "存储", value: formatAssetKind(selectedAsset.storageKind) },
      { label: "来源", value: formatAssetKind(selectedAsset.sourceKind) },
      {
        label: "SHA-256",
        value: formatAssetHash(selectedAsset.sha256),
        title: String(selectedAsset.sha256 || "").trim(),
      },
      {
        label: "原始链接",
        value: originalUrl,
        title: originalUrl,
        url: /^(https?:\/\/)/i.test(originalUrl) ? originalUrl : undefined,
      },
    ].filter((item) => item.value);
  }, [selectedAsset]);
  const selectedAssetIdSet = useMemo(
    () => new Set(selectedAssetIds),
    [selectedAssetIds],
  );
  useEffect(() => {
    setSelectedAssetIds((current) =>
      current.filter((id) => items.some((item) => item.id === id)),
    );
  }, [items]);

  useEffect(() => {
    if (!deleteDialogState) {
      setDeleteFileToo(false);
    }
  }, [deleteDialogState]);

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasMore || isLoading || isLoadingMore) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          void refreshAssets("append");
        }
      },
      {
        rootMargin: "320px 0px",
      },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [hasMore, isLoading, isLoadingMore, refreshAssets]);

  const handleToggleFavorite = async (asset: ImageAsset) => {
    try {
      const result = await updateUnifiedImageAsset(asset.id, {
        favorite: !asset.favorite,
      });
      setItems((current) =>
        current.map((item) => (item.id === asset.id ? result.item : item)),
      );
      if (selectedAsset?.id === asset.id) {
        setSelectedAsset(result.item);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "更新收藏失败");
    }
  };

  const handleSaveMetadata = async () => {
    if (!selectedAsset) {
      return;
    }
    setIsSaving(true);
    try {
      const result = await updateUnifiedImageAsset(selectedAsset.id, {
        category: editingCategory,
        tags: splitTagsInput(editingTags),
        note: editingNote,
        favorite: Boolean(selectedAsset.favorite),
      });
      setSelectedAsset(result.item);
      setItems((current) =>
        current.map((item) => (item.id === result.item.id ? result.item : item)),
      );
      toast.success("图片信息已更新");
      await refreshAssets("reset");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存失败");
    } finally {
      setIsSaving(false);
    }
  };

  const toggleAssetSelection = (assetId: string) => {
    setSelectedAssetIds((current) =>
      current.includes(assetId)
        ? current.filter((item) => item !== assetId)
        : [...current, assetId],
    );
  };

  const handleSelectAllVisible = () => {
    setSelectedAssetIds((current) => {
      if (items.length > 0 && current.length === items.length) {
        return [];
      }
      return items.map((item) => item.id);
    });
  };

  const handleBulkSave = async () => {
    if (selectedAssetIds.length === 0) {
      toast.warning("请先选择图片");
      return;
    }
    setIsBulkSaving(true);
    try {
      await updateUnifiedImageAssetsBulk({
        ids: selectedAssetIds,
        category: bulkCategory,
        tags: splitTagsInput(bulkTags),
      });
      toast.success(`已批量更新 ${selectedAssetIds.length} 张图片`);
      setBulkCategory("");
      setBulkTags("");
      await refreshAssets("reset");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "批量更新失败");
    } finally {
      setIsBulkSaving(false);
    }
  };

  const openSingleDeleteDialog = (asset: ImageAsset) => {
    setDeleteDialogState({
      mode: "single",
      asset,
    });
  };

  const openBulkDeleteDialog = () => {
    if (selectedAssetIds.length === 0) {
      toast.warning("请先选择图片");
      return;
    }
    setDeleteDialogState({
      mode: "bulk",
      ids: selectedAssetIds,
      count: selectedAssetIds.length,
    });
  };

  const handleConfirmDelete = async () => {
    if (!deleteDialogState) {
      return;
    }
    setIsDeleting(true);
    try {
      if (deleteDialogState.mode === "single") {
        const asset = deleteDialogState.asset;
        const result = await deleteUnifiedImageAsset(asset.id, {
          deleteFile: deleteFileToo,
        });
        const deletedFile = deleteFileToo && result.deletedFile;
        toast.success(deletedFile ? "图片和源文件已删除" : "图片记录已删除");
        setItems((current) => current.filter((item) => item.id !== asset.id));
        setSelectedAssetIds((current) => current.filter((id) => id !== asset.id));
        if (selectedAsset?.id === asset.id) {
          setSelectedAsset(null);
        }
      } else {
        const result = await deleteUnifiedImageAssetsBulk({
          ids: deleteDialogState.ids,
          deleteFile: deleteFileToo,
        });
        const deletedCount = result.items.length || deleteDialogState.count;
        const deletedFilesCount = result.deletedFiles?.length ?? 0;
        toast.success(
          deleteFileToo
            ? `已删除 ${deletedCount} 条记录，清理 ${deletedFilesCount} 个文件`
            : `已删除 ${deletedCount} 条图片记录`,
        );
        const deletedIdSet = new Set(deleteDialogState.ids);
        setItems((current) =>
          current.filter((item) => !deletedIdSet.has(item.id)),
        );
        setSelectedAssetIds((current) =>
          current.filter((id) => !deletedIdSet.has(id)),
        );
        if (selectedAsset && deletedIdSet.has(selectedAsset.id)) {
          setSelectedAsset(null);
        }
      }
      setDeleteDialogState(null);
      await refreshAssets("reset");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除失败");
    } finally {
      setIsDeleting(false);
    }
  };

  const handlePickImportFiles = () => {
    importInputRef.current?.click();
  };

  const handleImportFiles = async (files: FileList | null) => {
    const normalizedFiles = files ? Array.from(files) : [];
    if (normalizedFiles.length === 0) {
      return;
    }
    setIsImporting(true);
    try {
      const data = await importUnifiedImageAssets(normalizedFiles, {
        category: importCategory,
        tags: splitTagsInput(importTags),
        note: importNote,
      });
      const summary = countImportErrors(data);
      await refreshAssets("reset");
      setImportCategory("");
      setImportTags("");
      setImportNote("");
      if (summary.failed > 0) {
        toast.error(
          `已导入 ${summary.imported} 张，失败 ${summary.failed} 张${summary.skipped > 0 ? `，跳过 ${summary.skipped} 张` : ""}`,
        );
      } else if (summary.skipped > 0) {
        toast.success(
          `已导入 ${summary.imported} 张，跳过 ${summary.skipped} 张重复图片`,
        );
      } else {
        toast.success(`已导入 ${summary.imported} 张图片`);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "导入图片失败");
    } finally {
      setIsImporting(false);
      if (importInputRef.current) {
        importInputRef.current.value = "";
      }
    }
  };

  return (
    <section className="h-full">
      <div className="hide-scrollbar h-full overflow-y-auto rounded-[30px] border border-stone-200 bg-[radial-gradient(circle_at_top_left,_rgba(255,255,255,0.96),_rgba(246,242,236,0.98)_42%,_rgba(238,231,220,0.98)_100%)] px-4 pb-6 pt-5 shadow-[0_18px_55px_-28px_rgba(73,52,28,0.28)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 lg:px-6">
        <section className="flex flex-col gap-4 border-b border-stone-200/80 pb-5">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <div className="text-xs font-semibold uppercase tracking-[0.28em] text-stone-500">
                Image Library
              </div>
              <h1 className="mt-2 text-3xl font-semibold tracking-tight text-stone-950 dark:text-[var(--studio-text-strong)]">
                图片资产库
              </h1>
              <p className="mt-2 max-w-[760px] text-sm leading-7 text-stone-600 dark:text-[var(--studio-text-muted)]">
                为图片保留提示词、分类、标签、备注和收藏状态。这里按图片资产管理，不再只按会话浏览。
              </p>
            </div>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
              <div className="rounded-[24px] border border-stone-200/80 bg-white/80 px-4 py-3 text-sm text-stone-600 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">
                已加载 {items.length} / {total || items.length} 张匹配图片
              </div>
              <Button
                type="button"
                className="rounded-full bg-stone-950 text-white hover:bg-stone-800"
                onClick={handlePickImportFiles}
                disabled={isImporting}
              >
                {isImporting ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <Upload className="size-4" />
                )}
                导入图片
              </Button>
              <input
                ref={importInputRef}
                type="file"
                accept="image/*"
                multiple
                className="hidden"
                onChange={(event) => void handleImportFiles(event.target.files)}
              />
            </div>
          </div>

          <div className="grid gap-3 lg:grid-cols-[minmax(0,1.4fr)_200px_200px_180px_auto]">
            <label className="flex items-center gap-3 rounded-[22px] border border-stone-200 bg-white/85 px-4 py-3 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
              <Search className="size-4 text-stone-400" />
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索提示词、标签、分类、备注"
                className="h-7 w-full bg-transparent text-sm outline-none placeholder:text-stone-400"
              />
            </label>

            <select
              value={selectedCategory}
              onChange={(event) => setSelectedCategory(event.target.value)}
              className="h-[54px] rounded-[22px] border border-stone-200 bg-white/85 px-4 text-sm text-stone-700 shadow-sm outline-none dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)]"
            >
              <option value="">全部分类</option>
              {categories.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>

            <select
              value={selectedTag}
              onChange={(event) => setSelectedTag(event.target.value)}
              className="h-[54px] rounded-[22px] border border-stone-200 bg-white/85 px-4 text-sm text-stone-700 shadow-sm outline-none dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)]"
            >
              <option value="">全部标签</option>
              {allTags.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>

            <select
              value={selectedSort}
              onChange={(event) => setSelectedSort(event.target.value)}
              className="h-[54px] rounded-[22px] border border-stone-200 bg-white/85 px-4 text-sm text-stone-700 shadow-sm outline-none dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)]"
            >
              {SORT_OPTIONS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>

            <Button
              type="button"
              variant={favoriteOnly ? "default" : "outline"}
              className={cn(
                "h-[54px] rounded-[22px]",
                favoriteOnly
                  ? "bg-stone-950 text-white hover:bg-stone-800"
                  : "border-stone-200 bg-white/85 text-stone-700 hover:bg-white",
              )}
              onClick={() => setFavoriteOnly((current) => !current)}
            >
              <Heart className={cn("size-4", favoriteOnly && "fill-current")} />
              只看收藏
            </Button>
          </div>

          <div className="grid gap-3 rounded-[26px] border border-stone-200/80 bg-white/75 p-4 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] lg:grid-cols-[auto_220px_minmax(0,1fr)_auto]">
            <Button
              type="button"
              variant="outline"
              className="rounded-full border-stone-200 bg-white"
              onClick={handleSelectAllVisible}
            >
              {selectedAssetIds.length === items.length && items.length > 0 ? (
                <CheckSquare className="size-4" />
              ) : (
                <Square className="size-4" />
              )}
              {selectedAssetIds.length === items.length && items.length > 0
                ? "取消全选"
                : "全选当前结果"}
            </Button>

            <Input
              value={bulkCategory}
              onChange={(event) => setBulkCategory(event.target.value)}
              placeholder="批量分类"
              className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
            />

            <Input
              value={bulkTags}
              onChange={(event) => setBulkTags(event.target.value)}
              placeholder="批量标签，多个用逗号分隔"
              className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
            />

            <Button
              type="button"
              variant="outline"
              className="rounded-full border-rose-200 bg-white text-rose-600 hover:bg-rose-50"
              onClick={openBulkDeleteDialog}
              disabled={selectedAssetIds.length === 0 || isDeleting}
            >
              {isDeleting && deleteDialogState?.mode === "bulk" ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Trash2 className="size-4" />
              )}
              批量删除
            </Button>

            <Button
              type="button"
              className="rounded-full bg-stone-950 text-white hover:bg-stone-800"
              onClick={() => void handleBulkSave()}
              disabled={selectedAssetIds.length === 0 || isBulkSaving}
            >
              {isBulkSaving ? <LoaderCircle className="size-4 animate-spin" /> : null}
              批量更新
            </Button>
          </div>

          <div className="grid gap-3 rounded-[26px] border border-stone-200/80 bg-white/75 p-4 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] lg:grid-cols-[220px_minmax(0,1fr)]">
            <Input
              value={importCategory}
              onChange={(event) => setImportCategory(event.target.value)}
              placeholder="导入分类（可选）"
              className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
            />
            <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
              <Input
                value={importTags}
                onChange={(event) => setImportTags(event.target.value)}
                placeholder="导入标签，多个用逗号分隔（可选）"
                className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
              />
              <Input
                value={importNote}
                onChange={(event) => setImportNote(event.target.value)}
                placeholder="导入备注（可选）"
                className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
              />
            </div>
          </div>

          {tagStats.length > 0 ? (
            <div className="rounded-[26px] border border-stone-200/80 bg-white/72 p-4 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
              <div className="flex items-center gap-2 text-sm font-medium text-stone-800 dark:text-[var(--studio-text-strong)]">
                <Tags className="size-4 text-stone-500" />
                标签统计
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                {tagStats.map(({ tag, count }) => (
                  <button
                    key={tag}
                    type="button"
                    onClick={() =>
                      setSelectedTag((current) => (current === tag ? "" : tag))
                    }
                    className={cn(
                      "rounded-full px-3 py-1.5 text-xs font-medium transition",
                      selectedTag === tag
                        ? "bg-stone-950 text-white"
                        : "bg-[#f4eee5] text-[#77553d] hover:bg-[#eadfce]",
                    )}
                  >
                    #{tag} · {count}
                  </button>
                ))}
              </div>
            </div>
          ) : null}
        </section>

        <div className="pt-5">
          {isLoading ? (
            <div className="flex min-h-[320px] items-center justify-center gap-3 text-stone-500">
              <LoaderCircle className="size-5 animate-spin" />
              正在加载图片资产
            </div>
          ) : items.length === 0 ? (
            <div className="rounded-[28px] border border-dashed border-stone-300 bg-white/60 px-6 py-14 text-center text-sm leading-7 text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">
              当前没有匹配的图片。先去图片工作台生成一些图片，或者调整筛选条件。
            </div>
          ) : (
            <div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                {items.map((asset) => {
                  const imageURL = normalizeImageURL(asset.imageUrl);
                  return (
                    <article
                      key={asset.id}
                      className="group overflow-hidden rounded-[26px] border border-stone-200 bg-white/88 shadow-[0_18px_48px_-35px_rgba(73,52,28,0.32)] transition hover:-translate-y-0.5 hover:shadow-[0_22px_55px_-32px_rgba(73,52,28,0.4)] dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]"
                    >
                      <button
                        type="button"
                        onClick={() => setSelectedAsset(asset)}
                        className="block w-full text-left"
                      >
                        <div className="relative aspect-[4/3] overflow-hidden bg-stone-100">
                          {imageURL ? (
                            <AppImage
                              src={imageURL}
                              alt={asset.title}
                              className="h-full w-full object-cover transition duration-300 group-hover:scale-[1.015]"
                            />
                          ) : (
                            <div className="flex h-full items-center justify-center text-sm text-stone-400">
                              图片不可预览
                            </div>
                          )}
                          <button
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation();
                              void handleToggleFavorite(asset);
                            }}
                            className={cn(
                              "absolute right-3 top-3 inline-flex size-10 items-center justify-center rounded-full border border-white/70 bg-black/35 text-white backdrop-blur transition hover:bg-black/55",
                              asset.favorite && "bg-rose-500/85 hover:bg-rose-500",
                            )}
                            aria-label={asset.favorite ? "取消收藏" : "收藏图片"}
                            title={asset.favorite ? "取消收藏" : "收藏图片"}
                          >
                            <Heart className={cn("size-4", asset.favorite && "fill-current")} />
                          </button>
                          <button
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation();
                              toggleAssetSelection(asset.id);
                            }}
                            className={cn(
                              "absolute left-3 top-3 inline-flex size-10 items-center justify-center rounded-full border border-white/70 bg-black/35 text-white backdrop-blur transition hover:bg-black/55",
                              selectedAssetIdSet.has(asset.id) && "bg-stone-950/90",
                            )}
                            aria-label={selectedAssetIdSet.has(asset.id) ? "取消选择图片" : "选择图片"}
                            title={selectedAssetIdSet.has(asset.id) ? "取消选择图片" : "选择图片"}
                          >
                            {selectedAssetIdSet.has(asset.id) ? (
                              <CheckSquare className="size-4" />
                            ) : (
                              <Square className="size-4" />
                            )}
                          </button>
                          <button
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation();
                              openSingleDeleteDialog(asset);
                            }}
                            className="absolute bottom-3 right-3 inline-flex size-10 items-center justify-center rounded-full border border-white/70 bg-black/35 text-white backdrop-blur transition hover:bg-rose-500/85"
                            aria-label="删除图片"
                            title="删除图片"
                          >
                            <Trash2 className="size-4" />
                          </button>
                        </div>

                        <div className="space-y-3 px-4 py-4">
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <h2 className="truncate text-sm font-semibold text-stone-900 dark:text-[var(--studio-text-strong)]">
                                {asset.title}
                              </h2>
                              <p className="mt-1 text-xs text-stone-400">
                                {formatAssetTime(asset.createdAt)}
                              </p>
                            </div>
                            {asset.category ? (
                              <span className="rounded-full bg-stone-100 px-2.5 py-1 text-[11px] font-medium text-stone-600 dark:bg-[var(--studio-panel-muted)] dark:text-[var(--studio-text)]">
                                {asset.category}
                              </span>
                            ) : null}
                          </div>

                          <p className="line-clamp-3 text-sm leading-6 text-stone-600 dark:text-[var(--studio-text-muted)]">
                            {asset.prompt || "无提示词"}
                          </p>

                          {asset.tags && asset.tags.length > 0 ? (
                            <div className="flex flex-wrap gap-2">
                              {asset.tags.slice(0, 4).map((tag) => (
                                <span
                                  key={tag}
                                  className="rounded-full bg-[#f4eee5] px-2.5 py-1 text-[11px] font-medium text-[#77553d] dark:bg-[var(--studio-panel-muted)] dark:text-[var(--studio-text)]"
                                >
                                  #{tag}
                                </span>
                              ))}
                            </div>
                          ) : (
                            <div className="text-xs text-stone-400">尚未设置标签</div>
                          )}
                        </div>
                      </button>
                    </article>
                  );
                })}
              </div>
              <div className="pt-6">
                <div ref={loadMoreRef} className="h-4 w-full" />
                {isLoadingMore ? (
                  <div className="flex items-center justify-center gap-3 text-sm text-stone-500">
                    <LoaderCircle className="size-4 animate-spin" />
                    正在继续加载图片
                  </div>
                ) : hasMore ? (
                  <div className="flex flex-col items-center gap-3">
                    <div className="text-xs text-stone-500">向下滚动会自动继续加载</div>
                    <Button
                      type="button"
                      variant="outline"
                      className="rounded-full border-stone-200 bg-white"
                      onClick={() => void refreshAssets("append")}
                    >
                      继续加载
                    </Button>
                  </div>
                ) : items.length > 0 ? (
                  <div className="text-center text-xs text-stone-400">已经到底了</div>
                ) : null}
              </div>
            </div>
          )}
        </div>
      </div>

      <Dialog open={Boolean(selectedAsset)} onOpenChange={(open) => !open && setSelectedAsset(null)}>
        <DialogContent className="w-[min(96vw,1080px)] max-w-none overflow-hidden p-0">
          {selectedAsset ? (
            <div className="grid max-h-[86vh] grid-cols-1 overflow-hidden lg:grid-cols-[1.1fr_0.9fr]">
              <div className="overflow-auto bg-[#ede5db] p-4 dark:bg-[var(--studio-panel)]">
                {selectedImageURL ? (
                  <AppImage
                    src={selectedImageURL}
                    alt={selectedAsset.title}
                    className="mx-auto block max-h-[72vh] w-auto max-w-full rounded-[24px] shadow-[0_20px_55px_-28px_rgba(0,0,0,0.45)]"
                  />
                ) : (
                  <div className="flex min-h-[320px] items-center justify-center rounded-[24px] bg-white/70 text-sm text-stone-500">
                    图片不可预览
                  </div>
                )}
              </div>

              <div className="hide-scrollbar overflow-y-auto bg-white p-6 dark:bg-[var(--studio-panel-soft)]">
                <DialogHeader>
                  <DialogTitle>{selectedAsset.title}</DialogTitle>
                  <DialogDescription>
                    {formatAssetTime(selectedAsset.createdAt)} · {selectedAsset.model || "未知模型"}
                  </DialogDescription>
                </DialogHeader>

                <div className="mt-6 space-y-5">
                  <div className="space-y-2">
                    <label className="text-xs font-semibold uppercase tracking-[0.2em] text-stone-500">
                      分类
                    </label>
                    <Input
                      value={editingCategory}
                      onChange={(event) => setEditingCategory(event.target.value)}
                      placeholder="比如：角色、海报、摄影、概念图"
                      className="h-11 rounded-2xl border-stone-200 bg-stone-50 shadow-none"
                    />
                  </div>

                  <div className="space-y-2">
                    <label className="text-xs font-semibold uppercase tracking-[0.2em] text-stone-500">
                      标签
                    </label>
                    <Input
                      value={editingTags}
                      onChange={(event) => setEditingTags(event.target.value)}
                      placeholder="多个标签用逗号分隔"
                      className="h-11 rounded-2xl border-stone-200 bg-stone-50 shadow-none"
                    />
                  </div>

                  <div className="space-y-2">
                    <label className="text-xs font-semibold uppercase tracking-[0.2em] text-stone-500">
                      备注
                    </label>
                    <Textarea
                      value={editingNote}
                      onChange={(event) => setEditingNote(event.target.value)}
                      placeholder="补充风格、用途、客户备注等"
                      className="min-h-[120px] rounded-[22px] border-stone-200 bg-stone-50 shadow-none"
                    />
                  </div>

                  <div className="space-y-3">
                    <div className="flex items-center justify-between gap-3 rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3">
                      <div>
                        <div className="text-sm font-medium text-stone-900">提示词</div>
                        <div className="text-xs text-stone-500">保留原始提示词，方便复用</div>
                      </div>
                      <Button
                        type="button"
                        variant="outline"
                        className="rounded-full border-stone-200 bg-white"
                        onClick={() => void copyText(selectedAsset.prompt, "提示词已复制")}
                      >
                        <Copy className="size-4" />
                        复制
                      </Button>
                    </div>
                    <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-4 text-sm leading-7 text-stone-700">
                      {selectedAsset.prompt || "无提示词"}
                    </div>
                  </div>

                  {selectedAsset.revisedPrompt ? (
                    <div className="space-y-3">
                      <div className="flex items-center gap-2 text-sm font-medium text-stone-900">
                        <TextCursorInput className="size-4 text-stone-500" />
                        修订提示词
                      </div>
                      <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-4 text-sm leading-7 text-stone-700">
                        {selectedAsset.revisedPrompt}
                      </div>
                    </div>
                  ) : null}

                  {selectedAssetStorageDetails.length > 0 ? (
                    <div className="space-y-3">
                      <div className="flex items-center gap-2 text-sm font-medium text-stone-900">
                        <Database className="size-4 text-stone-500" />
                        存储信息
                      </div>
                      <dl className="grid grid-cols-1 gap-2 rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-4 text-xs sm:grid-cols-2">
                        {selectedAssetStorageDetails.map((item) => (
                          <div key={item.label} className="min-w-0">
                            <dt className="text-stone-400">{item.label}</dt>
                            <dd
                              className="mt-1 truncate font-medium text-stone-700"
                              title={item.title || item.value}
                            >
                              {item.url ? (
                                <a
                                  href={item.url}
                                  target="_blank"
                                  rel="noreferrer"
                                  className="underline decoration-stone-300 underline-offset-2 hover:text-stone-950"
                                >
                                  {item.value}
                                </a>
                              ) : (
                                item.value
                              )}
                            </dd>
                          </div>
                        ))}
                      </dl>
                    </div>
                  ) : null}

                  <div className="flex flex-wrap gap-3 text-xs text-stone-500">
                    {selectedAsset.category ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-stone-100 px-3 py-1.5">
                        <Tag className="size-3.5" />
                        {selectedAsset.category}
                      </span>
                    ) : null}
                    {selectedAsset.tags && selectedAsset.tags.length > 0 ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-stone-100 px-3 py-1.5">
                        <Tags className="size-3.5" />
                        {selectedAsset.tags.join(" / ")}
                      </span>
                    ) : null}
                  </div>
                </div>

                <DialogFooter className="mt-6 border-t border-stone-200 pt-5">
                  <Button
                    type="button"
                    variant="outline"
                    className="rounded-full border-rose-200 text-rose-600 hover:bg-rose-50"
                    onClick={() => openSingleDeleteDialog(selectedAsset)}
                  >
                    <Trash2 className="size-4" />
                    删除图片
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    className="rounded-full border-stone-200"
                    onClick={() =>
                      void handleToggleFavorite({
                        ...selectedAsset,
                        favorite: Boolean(selectedAsset.favorite),
                      })
                    }
                  >
                    <Heart
                      className={cn(
                        "size-4",
                        selectedAsset.favorite && "fill-current text-rose-500",
                      )}
                    />
                    {selectedAsset.favorite ? "取消收藏" : "收藏图片"}
                  </Button>
                  <Button
                    type="button"
                    className="rounded-full bg-stone-950 text-white hover:bg-stone-800"
                    onClick={() => void handleSaveMetadata()}
                    disabled={isSaving}
                  >
                    {isSaving ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : null}
                    保存信息
                  </Button>
                </DialogFooter>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(deleteDialogState)}
        onOpenChange={(open) => {
          if (!open && !isDeleting) {
            setDeleteDialogState(null);
          }
        }}
      >
        <DialogContent className="max-w-[460px] rounded-[28px] border-stone-200 bg-white p-0 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
          {deleteDialogState ? (
            <div className="p-6">
              <DialogHeader>
                <DialogTitle>
                  {deleteDialogState.mode === "single" ? "删除图片" : "批量删除图片"}
                </DialogTitle>
                <DialogDescription>
                  {deleteDialogState.mode === "single"
                    ? "默认只删除图库记录。"
                    : `将删除 ${deleteDialogState.count} 条图库记录。`}
                </DialogDescription>
              </DialogHeader>

              <div className="mt-5 space-y-4">
                <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 text-sm text-stone-700 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text)]">
                  {deleteDialogState.mode === "single"
                    ? deleteDialogState.asset.title || "未命名图片"
                    : `已选择 ${deleteDialogState.count} 张图片`}
                </div>

                <label className="flex items-start gap-3 rounded-[22px] border border-rose-200/70 bg-rose-50/70 px-4 py-3 text-sm text-stone-700 dark:border-rose-900/50 dark:bg-rose-950/10 dark:text-[var(--studio-text)]">
                  <Checkbox
                    checked={deleteFileToo}
                    onCheckedChange={(checked) => setDeleteFileToo(Boolean(checked))}
                    className="mt-0.5"
                  />
                  <span className="leading-6">同时删除源文件</span>
                </label>
              </div>

              <DialogFooter className="mt-6">
                <Button
                  type="button"
                  variant="outline"
                  className="rounded-full border-stone-200"
                  onClick={() => setDeleteDialogState(null)}
                  disabled={isDeleting}
                >
                  取消
                </Button>
                <Button
                  type="button"
                  className="rounded-full bg-rose-600 text-white hover:bg-rose-500"
                  onClick={() => void handleConfirmDelete()}
                  disabled={isDeleting}
                >
                  {isDeleting ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <Trash2 className="size-4" />
                  )}
                  确认删除
                </Button>
              </DialogFooter>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </section>
  );
}
