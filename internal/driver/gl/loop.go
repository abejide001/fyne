package gl

import (
	"runtime"
	"sync"
	"time"

	"fyne.io/fyne"
	"fyne.io/fyne/internal/driver"
	"github.com/go-gl/gl/v3.2-core/gl"
	"github.com/go-gl/glfw/v3.2/glfw"
)

type funcData struct {
	f    func()
	done chan bool
}

type withContext interface {
	runWithContext(f func())
}

// channel for queuing functions on the main thread
var funcQueue = make(chan funcData)
var runFlag = false
var runMutex = &sync.Mutex{}

// Arrange that main.main runs on main thread.
func init() {
	runtime.LockOSThread()
}

func running() bool {
	runMutex.Lock()
	defer runMutex.Unlock()
	return runFlag
}

// force a function f to run on the main thread
func runOnMain(f func()) {
	// If we are on main just execute - otherwise add it to the main queue and wait.
	// The "running" variable is normally false when we are on the main thread.
	if !running() {
		f()
	} else {
		done := make(chan bool)

		funcQueue <- funcData{f: f, done: done}
		<-done
	}
}

func (d *gLDriver) runGL() {
	fps := time.NewTicker(time.Second / 60)
	runMutex.Lock()
	runFlag = true
	runMutex.Unlock()

	settingsChange := make(chan fyne.Settings)
	fyne.CurrentApp().Settings().AddChangeListener(settingsChange)

	for {
		select {
		case <-d.done:
			fps.Stop()
			glfw.Terminate()
			return
		case f := <-funcQueue:
			f.f()
			if f.done != nil {
				f.done <- true
			}
		case <-settingsChange:
			clearFontCache()
		case <-fps.C:
			glfw.PollEvents()
			newWindows := []fyne.Window{}
			reassign := false
			for _, win := range d.windows {
				w := win.(*window)
				viewport := w.viewport

				if viewport.ShouldClose() {
					reassign = true
					// remove window from window list
					viewport.Destroy()

					if w.master {
						d.Quit()
					}
					continue
				} else {
					newWindows = append(newWindows, win)
				}

				canvas := w.canvas
				if !canvas.isDirty() {
					continue
				}

				w.runWithContext(func() {
					d.freeDirtyTextures(canvas)

					gl.UseProgram(canvas.program)

					updateGLContext(w)
					if canvas.ensureMinSize() {
						w.fitContent()
					}
					canvas.paint(canvas.Size())

					w.viewport.SwapBuffers()
				})
			}
			if reassign {
				d.windows = newWindows
			}
		}
	}
}

func (d *gLDriver) freeDirtyTextures(canvas *glCanvas) {
	for {
		select {
		case object := <-canvas.refreshQueue:
			freeWalked := func(obj fyne.CanvasObject, _ fyne.Position, _ fyne.Position, _ fyne.Size) bool {
				texture := textures[obj]
				if texture != 0 {
					gl.DeleteTextures(1, &texture)
					delete(textures, obj)
				}
				delete(canvas.minSizes, obj)
				return false
			}
			driver.WalkCompleteObjectTree(object, freeWalked, nil)
		default:
			return
		}
	}
}
