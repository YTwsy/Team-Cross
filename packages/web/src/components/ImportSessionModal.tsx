import { useEffect, useRef } from "react";
import { SessionPicker } from "./SessionPicker";

export function ImportSessionModal({
  open,
  onClose,
  onImport,
}: {
  open: boolean;
  onClose: () => void;
  onImport: (provider: string, sessionId: string) => Promise<void>;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (open) dialog.current?.showModal();
    else if (dialog.current?.open) dialog.current.close();
  }, [open]);
  return (
    <dialog
      className="modal surface review-modal"
      ref={dialog}
      onCancel={onClose}
      aria-labelledby="import-title"
    >
      <header>
        <div>
          <p className="eyebrow">Read-only context</p>
          <h2 id="import-title">导入 Native Session</h2>
        </div>
        <button
          aria-label="Close"
          className="modal-close"
          onClick={onClose}
          type="button"
        >
          ×
        </button>
      </header>
      {open ? (
        <SessionPicker
          includeTitle={false}
          submitLabel="保存只读快照"
          onImport={async (provider, sessionId) => {
            await onImport(provider, sessionId);
            onClose();
          }}
        />
      ) : null}
    </dialog>
  );
}
