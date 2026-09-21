// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation

@MainActor
class TunnelImporter {
    @MainActor
    private final class ImportResults {
        var configs = [TunnelConfiguration?]()
        var lastFileImportErrorText: (title: String, message: String)?
    }

    static func importFromFile(urls: [URL], into tunnelsManager: TunnelsManager, sourceVC: AnyObject?, errorPresenterType: ErrorPresenterProtocol.Type, completionHandler: (@MainActor () -> Void)? = nil) {
        guard !urls.isEmpty else {
            completionHandler?()
            return
        }
        let dispatchGroup = DispatchGroup()
        let results = ImportResults()
        for url in urls {
            if url.pathExtension.lowercased() == "zip" {
                dispatchGroup.enter()
                ZipImporter.importConfigFiles(from: url) { result in
                    switch result {
                    case .failure(let error):
                        results.lastFileImportErrorText = error.alertText
                    case .success(let configsInZip):
                        results.configs.append(contentsOf: configsInZip)
                    }
                    dispatchGroup.leave()
                }
            } else { /* if it is not a zip, we assume it is a conf */
                let fileName = url.lastPathComponent
                let fileBaseName = url.deletingPathExtension().lastPathComponent.trimmingCharacters(in: .whitespacesAndNewlines)
                dispatchGroup.enter()
                DispatchQueue.global(qos: .userInitiated).async {
                    let fileContents: String
                    do {
                        fileContents = try String(contentsOf: url)
                    } catch let error {
                        DispatchQueue.main.async {
                            if let cocoaError = error as? CocoaError, cocoaError.isFileError {
                                results.lastFileImportErrorText = (title: tr("alertCantOpenInputConfFileTitle"), message: error.localizedDescription)
                            } else {
                                results.lastFileImportErrorText = (title: tr("alertCantOpenInputConfFileTitle"), message: tr(format: "alertCantOpenInputConfFileMessage (%@)", fileName))
                            }
                            results.configs.append(nil)
                            dispatchGroup.leave()
                        }
                        return
                    }
                    var parseError: Error?
                    var tunnelConfiguration: TunnelConfiguration?
                    do {
                        tunnelConfiguration = try TunnelConfiguration(fromWgQuickConfig: fileContents, called: fileBaseName)
                    } catch let error {
                        parseError = error
                    }
                    let importedConfiguration = tunnelConfiguration
                    let importError = parseError
                    DispatchQueue.main.async {
                        if importError != nil {
                            if let parseError = importError as? WireGuardAppError {
                                results.lastFileImportErrorText = parseError.alertText
                            } else {
                                results.lastFileImportErrorText = (title: tr("alertBadConfigImportTitle"), message: tr(format: "alertBadConfigImportMessage (%@)", fileName))
                            }
                        }
                        results.configs.append(importedConfiguration)
                        dispatchGroup.leave()
                    }
                }
            }
        }
        dispatchGroup.notify(queue: .main) {
            tunnelsManager.addMultiple(tunnelConfigurations: results.configs.compactMap { $0 }) { numberSuccessful, lastAddError in
                if !results.configs.isEmpty && numberSuccessful == results.configs.count {
                    completionHandler?()
                    return
                }
                let alertText: (title: String, message: String)?
                if urls.count == 1 {
                    if urls.first!.pathExtension.lowercased() == "zip" && !results.configs.isEmpty {
                        alertText = (title: tr(format: "alertImportedFromZipTitle (%d)", numberSuccessful),
                                     message: tr(format: "alertImportedFromZipMessage (%1$d of %2$d)", numberSuccessful, results.configs.count))
                    } else {
                        alertText = results.lastFileImportErrorText ?? lastAddError?.alertText
                    }
                } else {
                    alertText = (title: tr(format: "alertImportedFromMultipleFilesTitle (%d)", numberSuccessful),
                                 message: tr(format: "alertImportedFromMultipleFilesMessage (%1$d of %2$d)", numberSuccessful, results.configs.count))
                }
                if let alertText = alertText {
                    errorPresenterType.showErrorAlert(title: alertText.title, message: alertText.message, from: sourceVC, onPresented: completionHandler)
                } else {
                    completionHandler?()
                }
            }
        }
    }
}
