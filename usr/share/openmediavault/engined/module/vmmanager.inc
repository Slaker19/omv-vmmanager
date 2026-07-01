<?php
class OMVModuleVmManager extends \OMV\Engine\Module\ServiceAbstract
    implements \OMV\Engine\Notify\IListener
{
    public function getName()
    {
        return 'VmManager';
    }

    public function bindListeners(\OMV\Engine\Notify\Dispatcher $dispatcher)
    {
        // When the vmmanager config changes in the DB, mark the service
        // as dirty so omv-salt deploy run vmmanager gets triggered on apply.
        $dispatcher->addListener(
            OMV_NOTIFY_MODIFY,
            'org.openmediavault.conf.service.vmmanager',
            [$this, 'setDirty']
        );
    }
}
