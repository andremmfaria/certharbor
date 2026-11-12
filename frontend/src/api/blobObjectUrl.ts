import { useEffect, useMemo } from 'react'

// The only other place an object URL is created besides downloadBlob.ts:
// that one triggers a download, this one feeds an <img src>. The URL is
// derived with useMemo (not useState+useEffect) so creating it is never a
// setState call inside an effect; the effect here only revokes the previous
// URL once a new blob (or none) replaces it, or on unmount.
export function useBlobObjectUrl(blob: Blob | undefined): string | undefined {
  const url = useMemo(() => (blob ? URL.createObjectURL(blob) : undefined), [blob])
  useEffect(() => {
    return () => {
      if (url) URL.revokeObjectURL(url)
    }
  }, [url])
  return url
}
