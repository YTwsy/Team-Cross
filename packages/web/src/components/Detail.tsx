import { useEffect, useState } from "react";
import { sessionStorage } from "../storage";
import { t } from "../i18n";
import { useResource } from "../api";
import type { Collaboration } from "../types";
import { ReadOnlySpace } from "./ReadOnlySpace";
import { ErrorBox, Loading } from "./ui";

export function Detail({
  id,
  executionId,
}: {
  id: string;
  executionId?: string;
}) {
  const resource = useResource<Collaboration>(`collaborations/${id}`, 2500);
  const [showInvite] = useState(
    () => !!sessionStorage.getItem(`teamcross.invite.${id}`),
  );
  useEffect(() => {
    sessionStorage.removeItem(`teamcross.invite.${id}`);
  }, [id]);
  const c =
    resource.dataPath === `collaborations/${id}` ? resource.data : undefined;
  if (!c)
    return (
      <>
        <ErrorBox message={resource.error} retry={resource.reload} />
        {resource.loading && <Loading />}
      </>
    );
  if (c.state === "unsupported")
    return (
      <>
        <a href="#/">{t("返回协作空间")}</a>
        <ErrorBox message={c.error} />
      </>
    );
  return (
    <ReadOnlySpace
      collaboration={c}
      executionId={executionId}
      reload={resource.reload}
      update={resource.setData}
      showInvite={showInvite}
    />
  );
}
