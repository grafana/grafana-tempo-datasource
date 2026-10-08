import { type monacoTypes } from '@grafana/ui';

import { setupAutoSize } from './TraceQLEditor';

function createEditor(contentHeight: number, width: number) {
  const container = document.createElement('div');
  const editorNode = document.createElement('div');
  container.appendChild(editorNode);
  let onContentSizeChange: (() => void) | undefined;

  const editor = {
    getContainerDomNode: () => container,
    getDomNode: () => editorNode,
    getContentHeight: jest.fn(() => contentHeight),
    getLayoutInfo: () => ({ width }),
    layout: jest.fn(),
    onDidContentSizeChange: (listener: () => void) => {
      onContentSizeChange = listener;
      return { dispose: () => {} };
    },
  };

  return {
    editor: editor as unknown as monacoTypes.editor.IStandaloneCodeEditor,
    container,
    editorNode,
    layout: editor.layout,
    setContentHeight: (height: number) => {
      editor.getContentHeight.mockReturnValue(height);
      onContentSizeChange?.();
    },
  };
}

describe('setupAutoSize', () => {
  it('sizes the container Monaco observes, so its height does not depend on the editor height', () => {
    const { editor, container, editorNode, layout } = createEditor(27, 800);

    setupAutoSize(editor);

    expect(container.style.height).toBe('27px');
    expect(editorNode.style.height).toBe('');
    expect(layout).toHaveBeenLastCalledWith({ width: 800, height: 27 });
  });

  it('follows content height changes and caps the height at 1000px', () => {
    const { editor, container, layout, setContentHeight } = createEditor(27, 800);
    setupAutoSize(editor);

    setContentHeight(46);
    expect(container.style.height).toBe('46px');
    expect(layout).toHaveBeenLastCalledWith({ width: 800, height: 46 });

    setContentHeight(5000);
    expect(container.style.height).toBe('1000px');
    expect(layout).toHaveBeenLastCalledWith({ width: 800, height: 1000 });
  });
});
