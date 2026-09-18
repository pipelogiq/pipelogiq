import { fireEvent, screen, within } from '@testing-library/react';

// jsdom lacks the browser methods used when Radix focuses a menu item.
if (!HTMLElement.prototype.scrollIntoView) HTMLElement.prototype.scrollIntoView = () => {};
if (!HTMLElement.prototype.hasPointerCapture) HTMLElement.prototype.hasPointerCapture = () => false;
if (!HTMLElement.prototype.setPointerCapture) HTMLElement.prototype.setPointerCapture = () => {};
if (!HTMLElement.prototype.releasePointerCapture) HTMLElement.prototype.releasePointerCapture = () => {};

// jsdom has no browser top layer. Its selector engine otherwise recursively
// calls Element.matches for :modal / :fullscreen when Floating UI positions a
// menu. Keep other selector and visibility behavior intact.
const nativeMatches = Element.prototype.matches;
Element.prototype.matches = function (selector: string): boolean {
  if (selector === ':modal' || selector === ':fullscreen' || selector === ':popover-open') return false;
  return nativeMatches.call(this, selector);
};

export function selectOption(trigger: HTMLElement, optionName: string): void {
  fireEvent.keyDown(trigger, { key: 'ArrowDown' });
  const listbox = screen.getByRole('listbox');
  const option = within(listbox).getByRole('option', { name: optionName });
  fireEvent.keyDown(option, { key: 'Enter' });
}
