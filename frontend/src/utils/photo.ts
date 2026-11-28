const MAX_WIDTH = 1200; // maxImageWidth in backend/internal/service/image.go

/**
 * Scales a photo down to the width the server stores anyway, so phones do not upload several MB.
 * Returns the original file if it is already small enough or the browser cannot decode it.
 */
export async function shrinkPhoto(file: File): Promise<Blob> {
  try {
    const bmp = await createImageBitmap(file, { imageOrientation: "from-image" });
    const scale = MAX_WIDTH / bmp.width;
    if (scale >= 1) {
      bmp.close();
      return file;
    }
    const canvas = document.createElement("canvas");
    canvas.width = MAX_WIDTH;
    canvas.height = Math.round(bmp.height * scale);
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    // JPEG has no alpha: put transparent photos on white instead of black.
    ctx.fillStyle = "#fff";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(bmp, 0, 0, canvas.width, canvas.height);
    bmp.close();
    const out = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.85));
    return out && out.size < file.size ? out : file;
  } catch {
    return file;
  }
}
