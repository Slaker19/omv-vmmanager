<?php
/**
 * omv-vmmanager OMV plugin — log-file registration.
 *
 * Registers the backend log file with OMV's "Diagnostics → System Logs"
 * page so operators can browse omv-vmmanager logs through the OMV UI.
 */
\OMV\System\LogFileSpec::registerSpecification('omv-vmmanager', [
    'filename' => 'omv-vmmanager.log',
    'filepath' => '/var/log/vmmanager/backend.log',
    'regex'    => '/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z?)\s+(\w+)\s+(.*)$/',
    'columns'  => [
        'Date & Time' => 'ts',
        'Level'       => 'level',
        'Message'     => 'msg',
    ],
]);
