const imageStateAttribute = 'data-chat-image-state';
const boundImages = new WeakSet<HTMLImageElement>();

/** Hide answer images until the browser has decoded the actual resource. */
export function prepareChatImage(element: Element): void {
  if (element.tagName === 'IMG') element.setAttribute(imageStateAttribute, 'pending');
}

function syncImage(img: HTMLImageElement): void {
  // The authenticated loader's transparent placeholder is not the source image.
  const placeholder = img.hasAttribute('data-protected-src') && !img.src.startsWith('blob:');
  const state = placeholder || !img.complete ? 'pending' : img.naturalWidth > 0 ? 'loaded' : 'failed';
  img.setAttribute(imageStateAttribute, state);
  const parent = img.parentElement;
  if (parent?.tagName === 'P' && parent.children.length === 1 && !parent.textContent?.trim()) {
    parent.setAttribute(imageStateAttribute, state);
  }
}

export function hydrateChatImageVisibility(root: ParentNode): void {
  // A streaming patch can add text or replace an image with a download card.
  root.querySelectorAll<HTMLParagraphElement>(`p[${imageStateAttribute}]`).forEach(parent => {
    if (parent.children.length !== 1 || parent.textContent?.trim()
      || !parent.firstElementChild?.hasAttribute(imageStateAttribute)) {
      parent.removeAttribute(imageStateAttribute);
    }
  });
  root.querySelectorAll<HTMLImageElement>(`img[${imageStateAttribute}]`).forEach(img => {
    if (!boundImages.has(img)) {
      boundImages.add(img);
      img.addEventListener('load', () => syncImage(img));
      img.addEventListener('error', () => syncImage(img));
    }
    // Cached loads can finish before the component's mounted/updated hook.
    syncImage(img);
  });
}
