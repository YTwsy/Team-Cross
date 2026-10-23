import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, errorText } from "../api";
import { t, tr } from "../i18n";
import { ErrorBox, Modal } from "./ui";

export type ReaderImage = {
  quote?: string;
  id: string;
  name: string;
  mime: string;
  hash: string;
  startOffset: number;
  endOffset: number;
};
export type ReaderResource = { path: string; body?: Record<string, unknown> };

export function MediaZoom({ children }: { children: ReactNode }) {
  const [view, setView] = useState({ scale: 1, x: 0, y: 0 });
  const viewport = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: number; x: number; y: number } | undefined>(
    undefined,
  );
  const zoom = (factor: number, x = 0, y = 0) =>
    setView((v) => {
      const scale = Math.max(0.25, Math.min(8, v.scale * factor));
      return {
        scale,
        x: x - ((x - v.x) * scale) / v.scale,
        y: y - ((y - v.y) * scale) / v.scale,
      };
    });
  useEffect(() => {
    const el = viewport.current;
    if (!el) return;
    const wheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = el.getBoundingClientRect();
      zoom(
        Math.exp(-event.deltaY * 0.002),
        event.clientX - rect.left - rect.width / 2,
        event.clientY - rect.top - rect.height / 2,
      );
    };
    el.addEventListener("wheel", wheel, { passive: false });
    return () => el.removeEventListener("wheel", wheel);
  }, []);
  return (
    <div className="reader-media-zoom">
      <div className="reader-rich-tools">
        <button
          type="button"
          onClick={() => zoom(1 / 1.25)}
          aria-label={t("缩小")}
        >
          −
        </button>
        <output>{Math.round(view.scale * 100)}%</output>
        <button type="button" onClick={() => zoom(1.25)} aria-label={t("放大")}>
          +
        </button>
        <button type="button" onClick={() => setView({ scale: 1, x: 0, y: 0 })}>
          {t("重置视图")}
        </button>
        <span className="muted">{t("滚轮缩放，拖动平移")}</span>
      </div>
      <div
        ref={viewport}
        className="reader-media-viewport"
        tabIndex={0}
        role="region"
        aria-label={t("图片缩放与平移")}
        onKeyDown={(e) => {
          if (e.key === "+" || e.key === "=") zoom(1.25);
          else if (e.key === "-") zoom(1 / 1.25);
          else if (e.key === "0") setView({ scale: 1, x: 0, y: 0 });
          else if (
            ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)
          ) {
            setView((v) => ({
              ...v,
              x:
                v.x +
                (e.key === "ArrowLeft" ? 40 : e.key === "ArrowRight" ? -40 : 0),
              y:
                v.y +
                (e.key === "ArrowUp" ? 40 : e.key === "ArrowDown" ? -40 : 0),
            }));
          } else return;
          e.preventDefault();
        }}
        onPointerDown={(e) => {
          if (e.button !== 0) return;
          e.currentTarget.setPointerCapture?.(e.pointerId);
          drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY };
        }}
        onPointerMove={(e) => {
          const d = drag.current;
          if (!d || d.id !== e.pointerId) return;
          setView((v) => ({
            ...v,
            x: v.x + e.clientX - d.x,
            y: v.y + e.clientY - d.y,
          }));
          drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY };
        }}
        onPointerUp={() => {
          drag.current = undefined;
        }}
        onPointerCancel={() => {
          drag.current = undefined;
        }}
      >
        <div
          className="reader-media-transform"
          style={{
            transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
          }}
        >
          {children}
        </div>
      </div>
    </div>
  );
}

async function imageData(
  resource: ReaderResource,
  image: ReaderImage,
  signal: AbortSignal,
) {
  let offset = 0,
    data = "";
  do {
    const args = { attachmentId: image.id, attachmentOffset: offset };
    const path = resource.body
      ? resource.path
      : `${resource.path}&${new URLSearchParams(Object.entries(args).map(([k, v]) => [k, String(v)]))}`;
    const result = await api<{
      attachment: ReaderImage & { data: string; nextOffset: number };
    }>(path, resource.body ? { ...resource.body, ...args } : undefined, signal);
    const chunk = result.attachment;
    if (
      !chunk ||
      chunk.id !== image.id ||
      chunk.hash !== image.hash ||
      chunk.mime !== image.mime ||
      (chunk.nextOffset && chunk.nextOffset <= offset) ||
      data.length + chunk.data.length > 8 * 1024 * 1024
    )
      throw new Error(t("图片内容已变化，请重新读取。"));
    data += chunk.data;
    offset = chunk.nextOffset;
  } while (offset);
  if (!/^image\/(png|jpeg|gif|webp)$/.test(image.mime))
    throw new Error(t("图片格式不支持"));
  return `data:${image.mime};base64,${data}`;
}

function AttachmentImage({
  image,
  resource,
  eager,
  onOpen,
}: {
  image: ReaderImage;
  resource: ReaderResource;
  eager?: boolean;
  onOpen?: () => void;
}) {
  const [visible, setVisible] = useState(!!eager);
  const [src, setSrc] = useState("");
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const element = useRef<HTMLDivElement>(null);
  const key = JSON.stringify([resource, image.id, image.hash]);
  useEffect(() => {
    if (eager || typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    if (element.current) observer.observe(element.current);
    return () => observer.disconnect();
  }, [eager]);
  useEffect(() => {
    if (!visible) return;
    const abort = new AbortController();
    setSrc("");
    setError("");
    void imageData(resource, image, abort.signal)
      .then((value) => {
        if (!abort.signal.aborted) setSrc(value);
      })
      .catch((e) => {
        if (!abort.signal.aborted) setError(errorText(e));
      });
    return () => abort.abort();
  }, [visible, key, retry]);
  return (
    <div ref={element} className="reader-attachment-image">
      {error ? (
        <ErrorBox message={error} retry={() => setRetry((n) => n + 1)} />
      ) : src ? (
        onOpen ? (
          <button
            className="reader-image-open"
            onClick={onOpen}
            aria-label={tr`展开图片：${image.name}`}
          >
            <img
              src={src}
              alt={image.name}
              onError={() => setError(t("图片加载失败"))}
            />
          </button>
        ) : (
          <img
            src={src}
            alt={image.name}
            draggable={false}
            onError={() => setError(t("图片加载失败"))}
          />
        )
      ) : (
        <span role="status">{t("正在读取图片…")}</span>
      )}
    </div>
  );
}

export function ReaderImages({
  images,
  resource,
  onAnnotate,
}: {
  images: ReaderImage[];
  resource: ReaderResource;
  onAnnotate?: (image: ReaderImage) => void;
}) {
  const [active, setActive] = useState<number>();
  const current = active === undefined ? undefined : images[active];
  return (
    <div className="reader-images">
      {images.map((image, index) => (
        <figure key={image.id}>
          <AttachmentImage
            image={image}
            resource={resource}
            onOpen={() => setActive(index)}
          />
          <figcaption>
            <span>{image.name}</span>
            {onAnnotate && (
              <button className="text-button" onClick={() => onAnnotate(image)}>
                {t("批注图片")}
              </button>
            )}
          </figcaption>
        </figure>
      ))}
      {current && (
        <Modal title={current.name} wide onClose={() => setActive(undefined)}>
          <div className="reader-rich-tools">
            <button
              disabled={active === 0}
              onClick={() => setActive(active! - 1)}
            >
              {t("上一张")}
            </button>
            <span>
              {active! + 1} / {images.length}
            </span>
            <button
              disabled={active === images.length - 1}
              onClick={() => setActive(active! + 1)}
            >
              {t("下一张")}
            </button>
          </div>
          <MediaZoom key={current.id}>
            <AttachmentImage image={current} resource={resource} eager />
          </MediaZoom>
        </Modal>
      )}
    </div>
  );
}
