package spacecontext

import "fmt"

func (ref SourceRef) IsShadow() bool {
	return ref.ShadowID != "" || ref.ShadowMessageID != "" || ref.ShadowResultID != ""
}

func (ref SourceRef) ValidateShadow() error {
	if ref.ShadowID == "" || (ref.ShadowMessageID == "") == (ref.ShadowResultID == "") || ref.MaterialID != "" || ref.Version != 0 || ref.AnnotationID != "" || ref.ReplyID != "" || ref.ExecutionID != "" || ref.TurnID != "" || len(ref.ItemIDs) > 0 || ref.RequestID != "" || ref.BriefRevision != 0 || ref.BriefItemID != "" {
		return fmt.Errorf("分身来源需要独立的准确消息或结果标识")
	}
	return nil
}
