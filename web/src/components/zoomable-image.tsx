"use client";

import type { CSSProperties } from "react";
import Zoom from "react-medium-image-zoom";

import { AppImage } from "@/components/app-image";
import { cn } from "@/lib/utils";

type ZoomableImageProps = {
  src: string;
  alt: string;
  width?: number;
  height?: number;
  className?: string;
  overlayClassName?: string;
  zoomMargin?: number;
};

export function ZoomableImage({
  src,
  alt,
  width,
  height,
  className,
  overlayClassName,
  zoomMargin = 24,
}: ZoomableImageProps) {
  return (
    <Zoom zoomMargin={zoomMargin}>
      <AppImage
        src={src}
        alt={alt}
        width={width}
        height={height}
        unoptimized
        className={cn("cursor-zoom-in", className)}
        data-zoomable-image="true"
        style={
          overlayClassName
            ? ({
                ["--rmiz-overlay-background" as string]: "rgba(17, 24, 39, 0.82)",
              } as CSSProperties)
            : undefined
        }
      />
    </Zoom>
  );
}
