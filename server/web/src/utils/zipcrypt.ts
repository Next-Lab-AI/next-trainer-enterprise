import { BlobReader, BlobWriter, ZipWriter } from "@zip.js/zip.js"

export function genDatasetPassword(): string {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  let bin = ""
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin)
}

export async function packEncryptedZip(
  files: File[],
  password: string,
  onProgress?: (percent: number) => void,
): Promise<Blob> {
  const totalBytes = files.reduce((sum, f) => sum + f.size, 0) || 1
  let doneBytes = 0
  const zipWriter = new ZipWriter(new BlobWriter("application/zip"), {
    password,
    encryptionStrength: 3,
    bufferedWrite: true,
  })
  try {
    for (const file of files) {
      const base = doneBytes
      await zipWriter.add(file.name, new BlobReader(file), {
        onprogress: onProgress
          ? (loaded: number) => onProgress(((base + loaded) / totalBytes) * 100)
          : undefined,
      })
      doneBytes += file.size
    }
    return await zipWriter.close()
  } catch (err) {
    await zipWriter.close().catch(() => undefined)
    throw err
  }
}
