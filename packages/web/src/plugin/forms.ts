// The WebGUI handles forms in React and sends JSON through api(). Sandboxed
// hosts may omit allow-forms, which suppresses native submit before React sees
// it. Preserve the existing form, validation and handler without navigation.
export function bindSandboxForms() {
  const click = (event: MouseEvent) => {
    if (event.defaultPrevented || !(event.target instanceof Element)) return;
    const button = event.target.closest("button, input");
    if (
      !(
        button instanceof HTMLButtonElement ||
        button instanceof HTMLInputElement
      ) ||
      button.type !== "submit" ||
      button.disabled ||
      !button.form
    )
      return;
    event.preventDefault();
    const form = button.form;
    if (form.noValidate || button.formNoValidate || form.reportValidity()) {
      form.dispatchEvent(
        new SubmitEvent("submit", {
          bubbles: true,
          cancelable: true,
          submitter: button,
        }),
      );
    }
  };
  document.addEventListener("click", click);
  return () => document.removeEventListener("click", click);
}
