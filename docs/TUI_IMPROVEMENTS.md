# TUI Improvements with Bubbles

The server TUI has been revamped using [Charm Bracelet's Bubbles](https://github.com/charmbracelet/bubbles) library, providing a professional and responsive terminal user interface.

## Key Features

### 1. **Responsive Layout**
- All components automatically resize based on terminal window size
- Tables expand to use available space
- Viewport adjusts height dynamically
- Info boxes scale proportionally

### 2. **Table Component**
- Professional table rendering for connections and blocklist
- Automatic column width distribution
- Built-in keyboard navigation
- Highlighted selected rows
- Scales to terminal width

### 3. **Magnifiable Viewport**
- Log viewport resizes with terminal
- Maintains readability at any size
- Scrollable content with vim-style keys
- Auto-scrolls to bottom on new logs

### 4. **Spinner Component**
- Animated loading indicator during connection attempts
- Visual feedback when retrying connections
- Consistent loading state indicators

### 5. **Help Component**
- Integrated contextual help system
- Toggle with `?` key
- Shows all available keybindings for current view

### 6. **Key Bindings**
- Structured key bindings using `bubbles/key`
- Vim-style navigation (hjkl) alongside arrow keys
- Consistent keyboard shortcuts across views

## Visual Enhancements

### Clean Design
- No icons - pure functional design
- Professional table layout
- Color-coded status indicators
- Clear visual hierarchy
- Responsive spacing and alignment

### Dynamic Sizing
- All components respond to terminal resize events
- Tables expand/contract with window
- Viewport height adjusts automatically
- Info boxes scale proportionally
- Works on any terminal size (minimum 80x24 recommended)

## New Features

### Window Size Detection
- Automatically detects terminal dimensions
- Recalculates layouts on resize
- Updates table column widths dynamically
- Adjusts viewport height in real-time

### Alternate Screen Buffer
- Uses alternate screen buffer (like vim/less)
- Doesn't pollute terminal history
- Clean exit returns to original screen
- Mouse support enabled

### Help System
- Press `?` to toggle contextual help
- Shows all available commands for current view
- Clear keyboard shortcut documentation

### Better Navigation
- Vim-style keybindings (hjkl) alongside arrow keys
- Tab cycling through views
- Improved table navigation with bubbles
- Smooth scrolling in viewport

### Enhanced UX
- Animated loading states with spinner
- Connection counts in view headers
- Better visual feedback for selections
- Cleaner nickname input overlay
- Responsive to terminal size changes

## Keyboard Shortcuts

### Global
- `Tab` - Switch between views (Dashboard → Connections → Blocklist)
- `?` - Toggle help
- `q` or `Ctrl+C` - Quit

### Dashboard
- `↑/↓` or `k/j` - Scroll logs
- Terminal resize automatically adjusts layout

### Connections View
- `↑/↓` or `k/j` - Navigate connections
- `x` - Disconnect selected connection
- `b` - Block selected IP
- `n` - Set nickname for selected connection
- Table expands to fill terminal width

### Blocklist View
- `↑/↓` or `k/j` - Navigate blocked IPs
- `u` or `x` - Unblock selected IP
- Table expands to fill terminal width

## Technical Details

### Dependencies
- `github.com/charmbracelet/bubbles/help` - Help system
- `github.com/charmbracelet/bubbles/key` - Key binding management
- `github.com/charmbracelet/bubbles/progress` - Progress bars (future use)
- `github.com/charmbracelet/bubbles/spinner` - Loading spinners
- `github.com/charmbracelet/bubbles/table` - Professional tables
- `github.com/charmbracelet/bubbles/textinput` - Text input
- `github.com/charmbracelet/bubbles/viewport` - Scrollable content

### Code Structure
- Window size tracking with `width` and `height` fields
- `updateSizes()` method recalculates all dimensions
- Dynamic column width calculation
- Responsive box sizing
- Centralized key binding definitions
- Helper methods for table updates
- Better separation of concerns
- Improved message handling with batched commands

### Window Resize Handling
- Listens for `tea.WindowSizeMsg`
- Updates all component dimensions
- Recalculates table column widths
- Adjusts viewport height
- Resizes info boxes proportionally

## Usage

```bash
# Start the server
tunnel-server start

# Open the monitor TUI (resizable)
tunnel-server monitor

# Resize your terminal - everything adapts!
```

## Responsive Behavior

The TUI adapts to your terminal size:

- **Minimum 80x24**: Recommended minimum, fully functional
- **Standard terminals**: Optimal experience with proper spacing
- **Large terminals**: Components expand to use available space
- **On resize**: All components update immediately

## Design Philosophy

- **Clean and functional** - No decorative icons, focus on data
- **Responsive** - Works on any terminal size
- **Keyboard-driven** - All actions accessible via keyboard
- **Professional** - Uses industry-standard bubbles components
- **Accessible** - Clear visual hierarchy and status indicators
