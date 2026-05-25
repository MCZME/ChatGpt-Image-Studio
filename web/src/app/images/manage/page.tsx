"use client";

import { useCallback, useEffect, useState } from "react";
import { LoaderCircle, PencilLine, ScanSearch, Tags, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  cleanupImageAssets,
  deleteImageAssetCategory,
  deleteImageAssetTag,
  fetchImageAssetStats,
  renameImageAssetCategory,
  renameImageAssetTag,
  type ImageAssetCleanupResponse,
  type ImageAssetCategoryStat,
  type ImageAssetTagStat,
} from "@/lib/api";

type RenameState = {
  kind: "tag" | "category";
  from: string;
  to: string;
};

type MergeConfirmState = {
  from: string;
  to: string;
  count: number;
};

type CleanupOptions = {
  removeOrphanFiles: boolean;
  removeMissingFileAssets: boolean;
};

export default function ImageLibraryManagePage() {
  const [tags, setTags] = useState<ImageAssetTagStat[]>([]);
  const [categories, setCategories] = useState<ImageAssetCategoryStat[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [savingKey, setSavingKey] = useState("");
  const [renameState, setRenameState] = useState<RenameState | null>(null);
  const [mergeConfirmState, setMergeConfirmState] = useState<MergeConfirmState | null>(null);
  const [cleanupOptions, setCleanupOptions] = useState<CleanupOptions>({
    removeOrphanFiles: true,
    removeMissingFileAssets: true,
  });
  const [cleanupResult, setCleanupResult] =
    useState<ImageAssetCleanupResponse | null>(null);
  const [isScanningCleanup, setIsScanningCleanup] = useState(false);
  const [isRunningCleanup, setIsRunningCleanup] = useState(false);

  const refresh = useCallback(async () => {
    setIsLoading(true);
    try {
      const result = await fetchImageAssetStats();
      setTags(result.tags);
      setCategories(result.categories);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取统计失败");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleRename = async () => {
    if (!renameState) {
      return;
    }
    const nextValue = renameState.to.trim();
    if (!nextValue) {
      toast.warning("新名称不能为空");
      return;
    }
    if (renameState.kind === "tag") {
      const existing = tags.find(
        (item) =>
          item.tag.toLocaleLowerCase() === nextValue.toLocaleLowerCase() &&
          item.tag.toLocaleLowerCase() !== renameState.from.toLocaleLowerCase(),
      );
      if (existing) {
        setMergeConfirmState({
          from: renameState.from,
          to: existing.tag,
          count: existing.count,
        });
        return;
      }
    }
    await submitRename(renameState.kind, renameState.from, nextValue);
  };

  const submitRename = async (
    kind: "tag" | "category",
    from: string,
    to: string,
  ) => {
    const actionKey = `${kind}:${from}`;
    setSavingKey(actionKey);
    try {
      if (kind === "tag") {
        await renameImageAssetTag(from, to);
      } else {
        await renameImageAssetCategory(from, to);
      }
      toast.success(kind === "tag" ? "标签已更新" : "分类已更新");
      setRenameState(null);
      setMergeConfirmState(null);
      await refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "重命名失败");
    } finally {
      setSavingKey("");
    }
  };

  const handleDelete = async (kind: "tag" | "category", value: string) => {
    const actionKey = `${kind}:${value}`;
    setSavingKey(actionKey);
    try {
      if (kind === "tag") {
        await deleteImageAssetTag(value);
      } else {
        await deleteImageAssetCategory(value);
      }
      toast.success(kind === "tag" ? "已删除标签" : "已清空该分类");
      await refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除失败");
    } finally {
      setSavingKey("");
    }
  };

  const runCleanup = async (dryRun: boolean) => {
    const hasTarget =
      cleanupOptions.removeOrphanFiles || cleanupOptions.removeMissingFileAssets;
    if (!hasTarget) {
      toast.warning("请至少选择一项清理内容");
      return;
    }
    if (dryRun) {
      setIsScanningCleanup(true);
    } else {
      setIsRunningCleanup(true);
    }
    try {
      const result = await cleanupImageAssets({
        dryRun,
        removeOrphanFiles: cleanupOptions.removeOrphanFiles,
        removeMissingFileAssets: cleanupOptions.removeMissingFileAssets,
      });
      setCleanupResult(result);
      if (dryRun) {
        toast.success(
          `扫描完成：${result.orphanFiles.length} 个未引用文件，${result.missingAssets.length} 条缺失记录`,
        );
      } else {
        toast.success(
          `清理完成：删除 ${result.removedFiles.length} 个文件，移除 ${result.removedAssetIds.length} 条记录`,
        );
        await refresh();
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "清理失败");
    } finally {
      if (dryRun) {
        setIsScanningCleanup(false);
      } else {
        setIsRunningCleanup(false);
      }
    }
  };

  return (
    <section className="h-full">
      <div className="hide-scrollbar h-full overflow-y-auto rounded-[30px] border border-stone-200 bg-[radial-gradient(circle_at_top_left,_rgba(255,255,255,0.96),_rgba(247,243,237,0.98)_42%,_rgba(239,233,223,0.98)_100%)] px-4 pb-6 pt-5 shadow-[0_18px_55px_-28px_rgba(73,52,28,0.28)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 lg:px-6">
        <div className="border-b border-stone-200/80 pb-5">
          <div className="text-xs font-semibold uppercase tracking-[0.28em] text-stone-500">
            Library Manage
          </div>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight text-stone-950 dark:text-[var(--studio-text-strong)]">
            标签与分类管理
          </h1>
          <p className="mt-2 max-w-[760px] text-sm leading-7 text-stone-600 dark:text-[var(--studio-text-muted)]">
            这里集中处理标签、分类和文件清理。
          </p>
        </div>

        {isLoading ? (
          <div className="flex min-h-[320px] items-center justify-center gap-3 text-stone-500">
            <LoaderCircle className="size-5 animate-spin" />
            正在加载统计
          </div>
        ) : (
          <div className="grid gap-5 pt-5 xl:grid-cols-2">
            <section className="rounded-[26px] border border-stone-200/80 bg-white/80 p-4 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
              <div className="flex items-center gap-2 text-sm font-medium text-stone-800 dark:text-[var(--studio-text-strong)]">
                <Tags className="size-4 text-stone-500" />
                标签
              </div>
              <div className="mt-4 space-y-3">
                {tags.length === 0 ? (
                  <div className="text-sm text-stone-500">当前还没有标签。</div>
                ) : (
                  tags.map((item) => {
                    const actionKey = `tag:${item.tag}`;
                    return (
                      <div
                        key={item.tag}
                        className="flex items-center gap-3 rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
                            #{item.tag}
                          </div>
                          <div className="text-xs text-stone-500">{item.count} 张图片</div>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          className="rounded-full border-stone-200 bg-white"
                          onClick={() =>
                            setRenameState({
                              kind: "tag",
                              from: item.tag,
                              to: item.tag,
                            })
                          }
                        >
                          <PencilLine className="size-4" />
                          重命名
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          className="rounded-full border-rose-200 bg-white text-rose-600 hover:bg-rose-50"
                          onClick={() => void handleDelete("tag", item.tag)}
                          disabled={savingKey === actionKey}
                        >
                          <Trash2 className="size-4" />
                          删除
                        </Button>
                      </div>
                    );
                  })
                )}
              </div>
            </section>

            <section className="rounded-[26px] border border-stone-200/80 bg-white/80 p-4 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
              <div className="flex items-center gap-2 text-sm font-medium text-stone-800 dark:text-[var(--studio-text-strong)]">
                <Tags className="size-4 text-stone-500" />
                分类
              </div>
              <div className="mt-4 space-y-3">
                {categories.length === 0 ? (
                  <div className="text-sm text-stone-500">当前还没有分类。</div>
                ) : (
                  categories.map((item) => {
                    const actionKey = `category:${item.category}`;
                    return (
                      <div
                        key={item.category}
                        className="flex items-center gap-3 rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
                            {item.category}
                          </div>
                          <div className="text-xs text-stone-500">{item.count} 张图片</div>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          className="rounded-full border-stone-200 bg-white"
                          onClick={() =>
                            setRenameState({
                              kind: "category",
                              from: item.category,
                              to: item.category,
                            })
                          }
                        >
                          <PencilLine className="size-4" />
                          重命名
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          className="rounded-full border-rose-200 bg-white text-rose-600 hover:bg-rose-50"
                          onClick={() => void handleDelete("category", item.category)}
                          disabled={savingKey === actionKey}
                        >
                          <Trash2 className="size-4" />
                          清空分类
                        </Button>
                      </div>
                    );
                  })
                )}
              </div>
            </section>

            <section className="rounded-[26px] border border-stone-200/80 bg-white/80 p-4 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] xl:col-span-2">
              <div className="flex items-center gap-2 text-sm font-medium text-stone-800 dark:text-[var(--studio-text-strong)]">
                <ScanSearch className="size-4 text-stone-500" />
                文件清理
              </div>

              <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-start">
                <div className="grid gap-3 sm:grid-cols-2">
                  <label className="flex items-start gap-3 rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 text-sm text-stone-700 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text)]">
                    <Checkbox
                      checked={cleanupOptions.removeOrphanFiles}
                      onCheckedChange={(checked) =>
                        setCleanupOptions((current) => ({
                          ...current,
                          removeOrphanFiles: Boolean(checked),
                        }))
                      }
                      className="mt-0.5"
                    />
                    <span className="leading-6">未引用文件</span>
                  </label>
                  <label className="flex items-start gap-3 rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 text-sm text-stone-700 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text)]">
                    <Checkbox
                      checked={cleanupOptions.removeMissingFileAssets}
                      onCheckedChange={(checked) =>
                        setCleanupOptions((current) => ({
                          ...current,
                          removeMissingFileAssets: Boolean(checked),
                        }))
                      }
                      className="mt-0.5"
                    />
                    <span className="leading-6">源文件缺失记录</span>
                  </label>
                </div>

                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    className="rounded-full border-stone-200 bg-white"
                    onClick={() => void runCleanup(true)}
                    disabled={isScanningCleanup || isRunningCleanup}
                  >
                    {isScanningCleanup ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : (
                      <ScanSearch className="size-4" />
                    )}
                    扫描
                  </Button>
                  <Button
                    type="button"
                    className="rounded-full bg-stone-950 text-white hover:bg-stone-800"
                    onClick={() => void runCleanup(false)}
                    disabled={isScanningCleanup || isRunningCleanup}
                  >
                    {isRunningCleanup ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : (
                      <Trash2 className="size-4" />
                    )}
                    执行清理
                  </Button>
                </div>
              </div>

              <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
                  <div className="text-xs text-stone-500">未引用文件</div>
                  <div className="mt-1 text-2xl font-semibold text-stone-900 dark:text-[var(--studio-text-strong)]">
                    {cleanupResult?.orphanFiles.length ?? "-"}
                  </div>
                </div>
                <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
                  <div className="text-xs text-stone-500">缺失记录</div>
                  <div className="mt-1 text-2xl font-semibold text-stone-900 dark:text-[var(--studio-text-strong)]">
                    {cleanupResult?.missingAssets.length ?? "-"}
                  </div>
                </div>
                <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
                  <div className="text-xs text-stone-500">已删文件</div>
                  <div className="mt-1 text-2xl font-semibold text-stone-900 dark:text-[var(--studio-text-strong)]">
                    {cleanupResult?.removedFiles.length ?? "-"}
                  </div>
                </div>
                <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-3 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
                  <div className="text-xs text-stone-500">已移除记录</div>
                  <div className="mt-1 text-2xl font-semibold text-stone-900 dark:text-[var(--studio-text-strong)]">
                    {cleanupResult?.removedAssetIds.length ?? "-"}
                  </div>
                </div>
              </div>

              {cleanupResult ? (
                <div className="mt-4 grid gap-3 lg:grid-cols-2">
                  <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-4 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
                    <div className="text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
                      未引用文件
                    </div>
                    <div className="mt-3 space-y-2">
                      {cleanupResult.orphanFiles.length === 0 ? (
                        <div className="text-sm text-stone-500">无</div>
                      ) : (
                        cleanupResult.orphanFiles.slice(0, 8).map((item) => (
                          <div
                            key={`${item.path || item.filename}`}
                            className="truncate text-sm text-stone-600 dark:text-[var(--studio-text-muted)]"
                            title={item.path || item.filename}
                          >
                            {item.filename}
                          </div>
                        ))
                      )}
                    </div>
                  </div>

                  <div className="rounded-[22px] border border-stone-200 bg-stone-50 px-4 py-4 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
                    <div className="text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
                      缺失记录
                    </div>
                    <div className="mt-3 space-y-2">
                      {cleanupResult.missingAssets.length === 0 ? (
                        <div className="text-sm text-stone-500">无</div>
                      ) : (
                        cleanupResult.missingAssets.slice(0, 8).map((item) => (
                          <div
                            key={item.id}
                            className="truncate text-sm text-stone-600 dark:text-[var(--studio-text-muted)]"
                            title={item.filename || item.title || item.id}
                          >
                            {item.filename || item.title || item.id}
                          </div>
                        ))
                      )}
                    </div>
                  </div>
                </div>
              ) : null}
            </section>
          </div>
        )}

        {renameState ? (
          <div className="fixed inset-x-0 bottom-4 z-40 mx-auto w-[min(92vw,620px)] rounded-[26px] border border-stone-200 bg-white p-4 shadow-[0_18px_60px_-30px_rgba(0,0,0,0.35)] dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
            <div className="text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
              {renameState.kind === "tag" ? "重命名标签" : "重命名分类"}
            </div>
            <div className="mt-3 grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
              <Input
                value={renameState.to}
                onChange={(event) =>
                  setRenameState((current) =>
                    current ? { ...current, to: event.target.value } : current,
                  )
                }
                className="h-11 rounded-2xl border-stone-200 bg-stone-50 shadow-none"
              />
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className="rounded-full border-stone-200 bg-white"
                  onClick={() => setRenameState(null)}
                >
                  取消
                </Button>
                <Button
                  type="button"
                  className="rounded-full bg-stone-950 text-white hover:bg-stone-800"
                  onClick={() => void handleRename()}
                  disabled={savingKey === `${renameState.kind}:${renameState.from}`}
                >
                  保存
                </Button>
              </div>
            </div>
          </div>
        ) : null}

        {mergeConfirmState ? (
          <div className="fixed inset-x-0 bottom-32 z-50 mx-auto w-[min(92vw,620px)] rounded-[26px] border border-amber-200 bg-[#fff8ee] p-4 shadow-[0_18px_60px_-30px_rgba(0,0,0,0.35)] dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
            <div className="text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
              这会合并标签
            </div>
            <p className="mt-2 text-sm leading-7 text-stone-600 dark:text-[var(--studio-text-muted)]">
              你正准备把标签 <span className="font-medium">#{mergeConfirmState.from}</span> 改成已经存在的
              <span className="font-medium"> #{mergeConfirmState.to}</span>。
              确认后，这两个标签会合并到一起，当前已有 {mergeConfirmState.count} 张图片使用目标标签。
            </p>
            <div className="mt-4 flex gap-2">
              <Button
                type="button"
                variant="outline"
                className="rounded-full border-stone-200 bg-white"
                onClick={() => setMergeConfirmState(null)}
              >
                再想想
              </Button>
              <Button
                type="button"
                className="rounded-full bg-stone-950 text-white hover:bg-stone-800"
                onClick={() =>
                  void submitRename("tag", mergeConfirmState.from, mergeConfirmState.to)
                }
                disabled={savingKey === `tag:${mergeConfirmState.from}`}
              >
                确认合并
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
