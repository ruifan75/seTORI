import { useCallback, useState, useSyncExternalStore, type Dispatch, type SetStateAction } from 'react';
import { onViewerChange, sameViewer, viewerID } from '../queryClient';

// Query の外へコピーした値は、利用者・権限が変わったレンダーで初期値へ戻す。
// 古い非同期処理が持つ setter も捨てる（effect で消すだけでは後から復活する）。
export function useViewerState<T>(initial: T | (() => T)): [T, Dispatch<SetStateAction<T>>] {
  const viewer = useSyncExternalStore(onViewerChange, viewerID, viewerID);
  const fresh = () => typeof initial === 'function' ? (initial as () => T)() : initial;
  const [state, setState] = useState(() => ({ viewer, value: fresh() }));
  const value = state.viewer === viewer ? state.value : fresh();
  if (state.viewer !== viewer) setState({ viewer, value });
  const setValue = useCallback<Dispatch<SetStateAction<T>>>((next) => {
    if (!sameViewer(viewer)) return;
    setState((previous) => ({
      viewer,
      value: typeof next === 'function' ? (next as (value: T) => T)(previous.value) : next,
    }));
  }, [viewer]);
  return [value, setValue];
}
